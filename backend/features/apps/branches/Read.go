package branches

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go/jetstream"
)

// Projections are what branches read: the projections, waiting for a
// commit to be projected, and a target's history.
type Projections interface {
	live.Store
	WaitFor(ctx context.Context, tenant uint64, projector, key string, seq uint64, timeout time.Duration) (live.Projected, error)
	History(ctx context.Context, tenant uint64, t live.Target, fn func(live.Event) bool) error
	Settle(ctx context.Context, tenant uint64, projector string) error
	Stream(ctx context.Context, tenant uint64, area live.Area) (jetstream.Stream, error)
	WatchDrafts(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *live.Entry, error)
	Registry() *extensions.Registry
}

// current is a target's value in a projection once its last committed
// event is folded: a branch record or commit read right after the command
// that wrote it.
func current(ctx context.Context, p Projections, tenant uint64, projector string, t live.Target) (live.Projected, bool, error) {
	var seq uint64
	if stream, err := p.Stream(ctx, tenant, t.Area); err == nil {
		if m, err := stream.GetLastMsgForSubject(ctx, t.Subjects(tenant)); err == nil {
			seq = m.Sequence
		}
	}
	if seq == 0 {
		return live.Projected{}, false, nil
	}
	v, err := p.WaitFor(ctx, tenant, projector, t.Key(), seq, 2*time.Second)
	return v, err == nil && v.Value != nil, err
}

// Deps are what the branches need, made after the registry.
type Deps struct {
	Projections Projections
}

// reader reads parts at commits, caching what never changes: a target
// folded up to a sequence.
type reader struct {
	deps  func() Deps
	reg   *extensions.Registry
	mu    sync.Mutex
	cache map[string]json.RawMessage
}

func newReader(reg *extensions.Registry, deps func() Deps) *reader {
	return &reader{deps: deps, reg: reg, cache: map[string]json.RawMessage{}}
}

// projectorOf is the branchable projector folding a part type.
func (r *reader) projectorOf(typ string) (live.Projector, bool) {
	for _, pr := range live.Projectors.All(r.reg) {
		if pr.Branchable && slices.Contains(pr.Types, typ) {
			return pr, true
		}
	}
	return live.Projector{}, false
}

// at is a target's value in its branchable projection after the events up
// to seq: its state frozen in a commit.
func (r *reader) at(ctx context.Context, tenant uint64, t live.Target, seq uint64) (json.RawMessage, error) {
	key := fmt.Sprintf("%d/%s/%d", tenant, t.Key(), seq)
	r.mu.Lock()
	v, ok := r.cache[key]
	r.mu.Unlock()
	if ok {
		return v, nil
	}
	pr, ok := r.projectorOf(t.Type)
	if !ok {
		return nil, fmt.Errorf("%s is not a part", t.Type)
	}
	var value json.RawMessage
	var failed error
	err := r.deps().Projections.History(ctx, tenant, t, func(e live.Event) bool {
		if e.Seq > seq {
			return false
		}
		next, skip, err := pr.Apply(live.Fold{Key: t.Key(), State: value, Event: e})
		if err != nil {
			failed = err
			return false
		}
		if !skip {
			value = next
		}
		return e.Seq < seq
	})
	if err == nil {
		err = failed
	}
	if err != nil {
		return nil, err
	}
	if live.IsRemoved(value) {
		value = nil
	}
	r.mu.Lock()
	if len(r.cache) > 4000 {
		r.cache = map[string]json.RawMessage{}
	}
	r.cache[key] = value
	r.mu.Unlock()
	return value, nil
}

// chain walks a commit and its parents (first parents), newest first,
// until fn returns false.
func (r *reader) chain(ctx context.Context, tenant uint64, id string, fn func(Commit) bool) error {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		c, ok := r.commit(ctx, tenant, id)
		if !ok {
			return log.Code("commit_not_found").Errorf("there is no commit %q", id)
		}
		if !fn(c) {
			return nil
		}
		id = c.Parent
	}
	return nil
}

// refAt is the part's entry in the nearest commit from id that names it.
func (r *reader) refAt(ctx context.Context, tenant uint64, id, typ, part string) (PartRef, bool, error) {
	var found PartRef
	ok := false
	err := r.chain(ctx, tenant, id, func(c Commit) bool {
		for _, p := range c.Parts {
			if p.Type == typ && p.ID == part {
				found, ok = p, true
				return false
			}
		}
		return true
	})
	return found, ok, err
}

// partsAt are every part a commit holds, by key: each part's nearest
// entry, removed ones left out.
func (r *reader) partsAt(ctx context.Context, tenant uint64, id string) (map[string]PartRef, error) {
	out := map[string]PartRef{}
	seen := map[string]bool{}
	err := r.chain(ctx, tenant, id, func(c Commit) bool {
		for _, p := range c.Parts {
			if !seen[p.Key()] {
				seen[p.Key()] = true
				if !p.Removed {
					out[p.Key()] = p
				}
			}
		}
		return true
	})
	return out, err
}

// PartAt is a part's state at a commit: ok is false when the commit does
// not hold it.
func (r *reader) PartAt(ctx context.Context, tenant uint64, commit string, t live.Target) (live.Projected, bool, error) {
	ref, ok, err := r.refAt(ctx, tenant, commit, t.Type, t.ID)
	if err != nil || !ok || ref.Removed {
		return live.Projected{}, false, err
	}
	v, err := r.at(ctx, tenant, live.Target{Area: live.AreaDefinitions, Type: t.Type, ID: ref.Target}, ref.Seq)
	if err != nil || v == nil {
		return live.Projected{}, false, err
	}
	return live.Projected{Seq: ref.Seq, Value: v}, true, nil
}

// ownerOf is a part's owner as its draft holds it.
func ownerOf(raw json.RawMessage) string {
	var d struct {
		Prefab, Graph, Entity struct{ Owner string }
	}
	if json.Unmarshal(raw, &d) != nil {
		return ""
	}
	return d.Prefab.Owner + d.Graph.Owner + d.Entity.Owner
}

// owned reports whether target t, with value v on main, is one of app's
// parts: its manifest, or a part it owns.
func owned(app string, t live.Target, v json.RawMessage) bool {
	if t.Type == "app" {
		return t.ID == app
	}
	return ownerOf(v) == apps.Owner(app)
}

// Base is the live.branching hook's Base: a part as branch b's head
// commit holds it. A part no commit of the app names is main's as it is
// now when the app does not own it (a library prefab, a built-in), and
// none when it does (made on main since).
func (r *reader) Base(ctx context.Context, tenant uint64, b live.Branch, projector string, t live.Target) (live.Projected, bool, error) {
	rec, ok := r.record(ctx, tenant, b.App, b.Name)
	if !ok {
		return live.Projected{}, false, nil
	}
	if rec.Head != "" {
		ref, found, err := r.refAt(ctx, tenant, rec.Head, t.Type, t.ID)
		if err != nil {
			return live.Projected{}, false, err
		}
		if found {
			if ref.Removed {
				return live.Projected{}, false, nil
			}
			v, err := r.at(ctx, tenant, live.Target{Area: live.AreaDefinitions, Type: t.Type, ID: ref.Target}, ref.Seq)
			return live.Projected{Seq: ref.Seq, Value: v}, v != nil, err
		}
	}
	v, ok, err := r.deps().Projections.Get(ctx, tenant, projector, t.Key())
	if err != nil || !ok || v.Value == nil || owned(b.App, t, v.Value) {
		return live.Projected{}, false, err
	}
	return v, true, nil
}

// Check is the live.branching hook's Check: a branch changes only an open
// branch's parts the app owns, or parts it makes.
func (r *reader) Check(ctx context.Context, tenant uint64, b live.Branch, projector string, t live.Target, state json.RawMessage) *live.Problem {
	rec, ok := r.record(ctx, tenant, b.App, b.Name)
	switch {
	case !ok:
		return &live.Problem{Code: "branch_not_found", Path: "branch", Message: fmt.Sprintf("the app %s has no branch %q", b.App, b.Name), Fix: "make it with app.branch.create {app, name}"}
	case rec.Status != StatusOpen:
		return &live.Problem{Code: "branch_closed", Path: "branch", Message: fmt.Sprintf("the branch %s is %s", b.Name, rec.Status), Fix: "make a new branch"}
	}
	if t.Type == "app" {
		if t.ID != b.App {
			return &live.Problem{Code: "not_on_branch", Path: "target", Message: fmt.Sprintf("the branch %s is the app %s's, not %s's", b.Name, b.App, t.ID)}
		}
		return nil
	}
	if state == nil {
		return nil // a part the branch makes
	}
	if owner := ownerOf(state); owner != apps.Owner(b.App) {
		who := "the tenant's library"
		if owner != "" {
			who = owner
		}
		return &live.Problem{Code: "not_owned", Path: "target", Message: fmt.Sprintf("%s %s is %s's, not the app %s's, so a branch of the app does not change it", t.Type, t.ID, who, b.App),
			Fix: fmt.Sprintf("copy it into the app (create a new %s owned by app:%s from it) and change the copy", t.Type, b.App)}
	}
	return nil
}

// Draft is one part's draft on a branch as it is now.
type Draft struct {
	PartRef
	Value json.RawMessage `json:"-"`
}

// drafts are the parts drafted on branch b: on a branch, its own drafts;
// on main, the app's manifest and the parts it owns.
func (r *reader) drafts(ctx context.Context, tenant uint64, b live.Branch) ([]Draft, error) {
	var out []Draft
	for _, pr := range live.Projectors.All(r.reg) {
		if !pr.Branchable {
			continue
		}
		// Every change committed so far is in the drafts read
		if err := r.deps().Projections.Settle(ctx, tenant, pr.ID); err != nil {
			return nil, err
		}
		for _, typ := range pr.Types {
			ctx, cancel := context.WithCancel(ctx)
			entries, err := r.deps().Projections.WatchDrafts(ctx, tenant, pr.ID, typ+".*")
			if err != nil {
				cancel()
				return nil, err
			}
			for e := range entries {
				if e == nil {
					break
				}
				if e.Deleted {
					continue
				}
				id := strings.TrimPrefix(e.Key, typ+".")
				part, on := live.SplitBranchID(id)
				t := live.Target{Area: live.AreaDefinitions, Type: typ, ID: part}
				switch {
				case b.IsMain() && on.IsMain() && !live.IsRemoved(e.Value.Value) && owned(b.App, t, e.Value.Value):
				case !b.IsMain() && on == b:
				default:
					continue
				}
				out = append(out, Draft{PartRef: PartRef{Type: typ, ID: part, Target: id, Seq: e.Value.Seq, Removed: live.IsRemoved(e.Value.Value)}, Value: e.Value.Value})
			}
			cancel()
		}
	}
	slices.SortFunc(out, func(a, b Draft) int { return strings.Compare(a.Key(), b.Key()) })
	return out, nil
}

// Changes are the parts a branch changed since its head: what a commit
// of it now would hold.
func (r *reader) Changes(ctx context.Context, tenant uint64, b live.Branch, head string) ([]PartRef, error) {
	drafted, err := r.drafts(ctx, tenant, b)
	if err != nil {
		return nil, err
	}
	at := map[string]PartRef{}
	if head != "" {
		if at, err = r.partsAt(ctx, tenant, head); err != nil {
			return nil, err
		}
	}
	out := []PartRef{}
	seen := map[string]bool{}
	for _, d := range drafted {
		seen[d.Key()] = true
		prev, ok := at[d.Key()]
		switch {
		case d.Removed && !ok:
			continue // made and removed on the branch
		case ok && !d.Removed && prev.Target == d.Target && prev.Seq == d.Seq:
			continue
		case ok && !d.Removed && r.same(ctx, tenant, prev, d):
			continue // changed back, or discarded
		}
		out = append(out, d.PartRef)
	}
	if b.IsMain() {
		// A part main deleted (a graph's draft) is gone from the drafts
		for key, p := range at {
			if !seen[key] {
				out = append(out, PartRef{Type: p.Type, ID: p.ID, Target: p.Target, Seq: p.Seq, Removed: true})
			}
		}
	}
	slices.SortFunc(out, func(a, b PartRef) int { return strings.Compare(a.Key(), b.Key()) })
	return out, nil
}

// same reports whether a draft holds what a commit's entry froze.
func (r *reader) same(ctx context.Context, tenant uint64, prev PartRef, d Draft) bool {
	v, err := r.at(ctx, tenant, live.Target{Area: live.AreaDefinitions, Type: prev.Type, ID: prev.Target}, prev.Seq)
	return err == nil && Equal(v, d.Value)
}

// Equal compares two part states as definitions: leaving out revs,
// authors and times, which a copy keeps but a change moves.
func Equal(a, b json.RawMessage) bool {
	return strip(a) == strip(b)
}

func strip(raw json.RawMessage) string {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	for _, k := range []string{"rev", "updatedBy", "updatedAt", "createdBy", "versions", "published", "publishedRev"} {
		delete(v, k)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// record is a branch's record as last committed.
func (r *reader) record(ctx context.Context, tenant uint64, app, name string) (Record, bool) {
	v, ok, err := current(ctx, r.deps().Projections, tenant, Projector, branchTarget(app, name))
	if err != nil || !ok {
		return Record{}, false
	}
	rec, err := decode[Record](v.Value)
	return rec, err == nil
}

// commit is a commit as last committed.
func (r *reader) commit(ctx context.Context, tenant uint64, id string) (Commit, bool) {
	if !live.ValidToken(id) {
		return Commit{}, false
	}
	v, ok, err := current(ctx, r.deps().Projections, tenant, CommitProjector, live.Target{Area: live.AreaDefinitions, Type: CommitType, ID: id})
	if err != nil || !ok {
		return Commit{}, false
	}
	c, err := decode[Commit](v.Value)
	return c, err == nil
}
