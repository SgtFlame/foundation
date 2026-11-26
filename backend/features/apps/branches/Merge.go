package branches

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/diff"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// A merge of one branch into another (forge 19e): the parts the source
// changed since the two branches' common ancestor, each with its
// candidate (the source's state, or both sides' changes merged
// structurally), the conflicts left and how they are resolved.
const (
	MergeType      = "appMerge" // id: <app>-m<tsid>
	MergeProjector = "apps.merge"
)

// Merge statuses.
const (
	MergeConflicts = "conflicts" // conflicts are open
	MergeReady     = "ready"     // no conflict open; waits for approval
	MergeLanded    = "landed"    // the candidates are the target's drafts; app.commit {merge} commits them
	MergeMerged    = "merged"
)

// Events on a merge.
const (
	EventMergeUpdated = "updated" // {parts}
	EventMergeLanded  = "landed"  // {}
	EventMergeDone    = "merged"  // {commit}
)

// MergePart is one part a merge brings.
type MergePart struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	// Both is true when the target changed it too, so it was merged
	// structurally; else the candidate is the source's state.
	Both        bool                       `json:"both,omitempty"`
	Removed     bool                       `json:"removed,omitempty"` // the candidate removes it
	Candidate   json.RawMessage            `json:"candidate,omitempty"`
	Conflicts   []diff.Conflict            `json:"conflicts"`
	Resolutions map[string]diff.Resolution `json:"resolutions,omitempty"`
	// Changes are what landing the candidate changes on the target
	Changes []diff.Change `json:"changes"`
}

// Merge is a merge of a branch into another, the apps.merge projection's
// value.
type Merge struct {
	ID         string      `json:"id"`
	App        string      `json:"app"`
	Source     string      `json:"source"`
	Target     string      `json:"target"`
	Base       string      `json:"base"` // the common ancestor commit
	SourceHead string      `json:"sourceHead"`
	TargetHead string      `json:"targetHead"`
	Parts      []MergePart `json:"parts"`
	Status     string      `json:"status"`
	Commit     string      `json:"commit,omitempty"` // the merge commit, once merged
	Approvals  []Approval  `json:"approvals,omitempty"`
	Kinds      []string    `json:"kinds,omitempty"`
	Approved   bool        `json:"approved,omitempty"`
	Waiting    []string    `json:"waiting,omitempty"`
	Open       int         `json:"open"` // conflicts open
	CreatedBy  uint64      `json:"createdBy,string"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

func mergeTarget(id string) live.Target {
	return live.Target{Area: live.AreaDefinitions, Type: MergeType, ID: id}
}

// AppOfMerge is a merge id's app: <app>-m<tsid>.
func AppOfMerge(id string) string {
	if i := strings.LastIndex(id, "-m"); i > 0 {
		return id[:i]
	}
	return id
}

// GetMerge is a merge as projected.
func GetMerge(ctx context.Context, store live.Store, tenant uint64, id string) (Merge, bool) {
	v, ok, err := store.Get(ctx, tenant, MergeProjector, MergeType+"."+id)
	if err != nil || !ok || v.Value == nil {
		return Merge{}, false
	}
	m, err := decode[Merge](v.Value)
	return m, err == nil
}

// settle sets what follows from a merge's parts and approvals.
func (m *Merge) settle() {
	m.Open = 0
	var pd []PartDiff
	for _, p := range m.Parts {
		m.Open += len(p.Conflicts)
		pd = append(pd, PartDiff{Type: p.Type, ID: p.ID, Changes: p.Changes})
	}
	raw, _ := json.Marshal(pd)
	m.Kinds = KindsOf(Commit{Structured: raw})
	m.Approved, m.Waiting = ApprovalStatus(Commit{Structured: raw, Approvals: m.Approvals})
	if m.Status == MergeLanded || m.Status == MergeMerged {
		return
	}
	m.Status = MergeReady
	if m.Open > 0 {
		m.Status = MergeConflicts
	}
}

func foldMerge(f live.Fold) (json.RawMessage, error) {
	var m Merge
	if f.State != nil {
		if err := json.Unmarshal(f.State, &m); err != nil {
			return nil, err
		}
	}
	e := f.Event
	switch e.Type {
	case EventCreated:
		v, err := decode[Merge](e.Data)
		if err != nil {
			return nil, err
		}
		m = v
		m.CreatedBy, m.CreatedAt = e.UserID, e.Time
	case EventMergeUpdated:
		v, err := decode[struct{ Parts []MergePart }](e.Data)
		if err != nil {
			return nil, err
		}
		m.Parts = v.Parts
	case EventApproved:
		v, err := decode[Approval](e.Data)
		if err != nil {
			return nil, err
		}
		v.By, v.At = e.UserID, e.Time
		m.Approvals = append(m.Approvals, v)
	case EventMergeLanded:
		m.Status = MergeLanded
	case EventMergeDone:
		v, err := decode[struct{ Commit string }](e.Data)
		if err != nil {
			return nil, err
		}
		m.Status, m.Commit = MergeMerged, v.Commit
	default:
		return f.State, nil
	}
	m.UpdatedAt = e.Time
	m.settle()
	return json.Marshal(m)
}

// ancestor is the nearest commit both heads reach: the target's commits,
// nearest first, searched among every commit the source reaches.
func (r *reader) ancestor(ctx context.Context, tenant uint64, source, target string) (string, error) {
	reach := func(head string, fn func(string) bool) error {
		queue, seen := []string{head}, map[string]bool{}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			if !fn(id) {
				return nil
			}
			c, ok := r.commit(ctx, tenant, id)
			if !ok {
				return fmt.Errorf("there is no commit %q", id)
			}
			queue = append(queue, c.Parent, c.MergeParent)
		}
		return nil
	}
	of := map[string]bool{}
	if err := reach(source, func(id string) bool { of[id] = true; return true }); err != nil {
		return "", err
	}
	found := ""
	err := reach(target, func(id string) bool {
		if of[id] {
			found = id
			return false
		}
		return true
	})
	return found, err
}

func sameRef(a, b PartRef, aok, bok bool) bool {
	if !aok || !bok {
		return aok == bok
	}
	return a.Target == b.Target && a.Seq == b.Seq
}

// state is a part's draft state at a commit, nil when the commit does not
// hold it.
func (r *reader) state(ctx context.Context, tenant uint64, commit string, typ, id string) (json.RawMessage, error) {
	if commit == "" {
		return nil, nil
	}
	v, ok, err := r.PartAt(ctx, tenant, commit, live.Target{Area: live.AreaDefinitions, Type: typ, ID: id})
	if err != nil || !ok {
		return nil, err
	}
	return v.Value, nil
}

// prepare works out a merge of source into target at their heads: the
// parts the source changed since the base, each merged with the target's
// changes, keeping the resolutions given (by part key, then path) whose
// paths still conflict.
func (r *reader) prepare(ctx context.Context, tenant uint64, m *Merge, keep map[string]map[string]diff.Resolution) error {
	base, err := r.ancestor(ctx, tenant, m.SourceHead, m.TargetHead)
	if err != nil {
		return err
	}
	if base == "" {
		return fmt.Errorf("the branches %s and %s share no commit", m.Source, m.Target)
	}
	m.Base = base
	pb, err := r.partsAt(ctx, tenant, base)
	if err != nil {
		return err
	}
	ps, err := r.partsAt(ctx, tenant, m.SourceHead)
	if err != nil {
		return err
	}
	pt, err := r.partsAt(ctx, tenant, m.TargetHead)
	if err != nil {
		return err
	}
	keys := map[string]PartRef{}
	for k, p := range pb {
		keys[k] = p
	}
	for k, p := range ps {
		keys[k] = p
	}
	m.Parts = []MergePart{}
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range keys {
			if !yield(k) {
				return
			}
		}
	}) {
		ref := keys[k]
		b, bok := pb[k]
		s, sok := ps[k]
		t, tok := pt[k]
		if sameRef(s, b, sok, bok) {
			continue // the source did not change it
		}
		sState, err := r.state(ctx, tenant, m.SourceHead, ref.Type, ref.ID)
		if err != nil {
			return err
		}
		tState, err := r.state(ctx, tenant, m.TargetHead, ref.Type, ref.ID)
		if err != nil {
			return err
		}
		part := MergePart{Type: ref.Type, ID: ref.ID, Conflicts: []diff.Conflict{}}
		candidate := sState
		if !sameRef(t, b, tok, bok) {
			part.Both = true
			bState, err := r.state(ctx, tenant, base, ref.Type, ref.ID)
			if err != nil {
				return err
			}
			part.Resolutions = keep[k]
			merged, conflicts := diff.Merge3(ref.Type, ref.ID, diff.Definition(ref.Type, bState), diff.Definition(ref.Type, tState), diff.Definition(ref.Type, sState), part.Resolutions)
			part.Conflicts = conflicts
			candidate = nil
			if merged != nil {
				wrap := tState
				if diff.Definition(ref.Type, wrap) == nil {
					wrap = sState
				}
				if candidate, err = diff.WithDefinition(ref.Type, wrap, merged); err != nil {
					return err
				}
			}
		}
		if diff.Definition(ref.Type, candidate) == nil {
			candidate, part.Removed = nil, true
		}
		part.Candidate = candidate
		part.Changes = diff.Part(ref.Type, ref.ID, tState, candidate)
		if len(part.Changes) == 0 && len(part.Conflicts) == 0 {
			continue // both sides made the same change
		}
		m.Parts = append(m.Parts, part)
	}
	return nil
}

func newMergeID(app string) string { return fmt.Sprintf("%s-m%d", app, features.NewTSID()) }

// contributeMerge registers the merge projector and its commands.
func contributeMerge(reg *extensions.Registry, r *reader) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: MergeProjector, Source: "apps"}, Title: "App merges",
		Description: "Every merge of one app branch into another (forge 19e), keyed appMerge.<app>-m<n>: {id, app, source, target, base, sourceHead, targetHead, parts: [{type, id, both, removed, candidate, conflicts, resolutions, changes}], status (conflicts, ready, landed, merged), commit, approvals, kinds, approved, waiting, open}",
		Area:        live.AreaDefinitions, Types: []string{MergeType}, Fold: foldMerge,
	})
	register := func(c live.Command, newTarget func(args features.ServiceArgs, raw json.RawMessage) string, decide func(d live.Decision, m Merge, has bool) ([]live.NewEvent, []live.Problem)) {
		c.Area, c.TargetType, c.Projection, c.NoBranch = live.AreaDefinitions, MergeType, MergeProjector, true
		c.NewTargetFor = newTarget
		c.Resource = func(p features.Principal, t live.Target) string { return apps.Resource(p, AppOfMerge(t.ID)) }
		c.Decide = func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			var m Merge
			has := d.State != nil && json.Unmarshal(d.State, &m) == nil && m.ID != ""
			events, problems := decide(d, m, has)
			if problems != nil {
				return nil, problems
			}
			d.NoUndo(AppOfMerge(d.Target.ID), "merges are kept for the history")
			return events, nil
		}
		live.Commands.Register(reg, c)
	}
	byMerge := func(_ features.ServiceArgs, raw json.RawMessage) string {
		var a struct{ Merge string }
		_ = json.Unmarshal(raw, &a)
		return a.Merge
	}
	need := func(m Merge, has bool, statuses ...string) []live.Problem {
		if !has {
			return problem("merge_not_found", "merge", "there is no such merge", "")
		}
		if !slices.Contains(statuses, m.Status) {
			return problem("merge_"+m.Status, "merge", fmt.Sprintf("the merge is %s", m.Status), "")
		}
		return nil
	}

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "app.merge.request", Source: "apps"}, Title: "Request an app merge",
		Description: "Prepares a merge of one app branch (source) into another (target, main by default) at their heads (forge 19e): every part the source changed since the two branches' common ancestor, as the source has it when the target did not change it, else both sides' changes merged structurally (apps.merge3: changes at different paths merge; one value set two ways, or an item removed on one side and changed on the other, is a conflict). Resolve conflicts with app.merge.resolve, approve with app.merge.approve, land with app.merge.complete and commit with app.commit {merge}. again names an earlier merge of the same branches whose resolutions are kept where their paths still conflict (a stale merge prepared again).",
		Args:        json.RawMessage(`{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"source":{"type":"string","description":"The branch merged from"},"target":{"type":"string","description":"The branch merged into; main when left out"},"again":{"type":"string","description":"An earlier merge whose resolutions are kept"}},"required":["app","source"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"app":"tasks","source":"refund-limits"}`)},
		Events:      []string{EventCreated}, Action: "app.write",
	}, func(args features.ServiceArgs, raw json.RawMessage) string {
		var a struct{ App string }
		_ = json.Unmarshal(raw, &a)
		return newMergeID(a.App)
	}, func(d live.Decision, _ Merge, has bool) ([]live.NewEvent, []live.Problem) {
		var a struct{ App, Source, Target, Again string }
		_ = json.Unmarshal(d.Args, &a)
		if has {
			return nil, problem("", "target", "a merge is requested once", "leave the target out")
		}
		if a.Target == "" {
			a.Target = live.Main
		}
		if a.Source == a.Target {
			return nil, problem("", "source", "a branch is not merged into itself", "")
		}
		ctx, tenant := d.Context(), d.Principal.TenantID
		src, ok := r.record(ctx, tenant, a.App, a.Source)
		if !ok || src.Status != StatusOpen {
			return nil, problem("branch_not_found", "source", fmt.Sprintf("the app %s has no open branch %q", a.App, a.Source), "")
		}
		tgt, ok := r.record(ctx, tenant, a.App, a.Target)
		if !ok || tgt.Status != StatusOpen {
			return nil, problem("branch_not_found", "target", fmt.Sprintf("the app %s has no open branch %q", a.App, a.Target), "")
		}
		if src.Head == "" || src.Head == src.Base.Commit && src.Commits == 0 {
			return nil, problem("nothing_to_merge", "source", fmt.Sprintf("the branch %s has no commit of its own", a.Source), "commit it first")
		}
		keep := map[string]map[string]diff.Resolution{}
		if a.Again != "" {
			old, ok := GetMerge(ctx, r.deps().Projections, tenant, a.Again)
			if !ok || old.Source != a.Source || old.Target != a.Target {
				return nil, problem("merge_not_found", "again", fmt.Sprintf("there is no merge %q of %s into %s", a.Again, a.Source, a.Target), "")
			}
			for _, p := range old.Parts {
				if len(p.Resolutions) > 0 {
					keep[p.Type+"."+p.ID] = p.Resolutions
				}
			}
		}
		m := Merge{ID: d.Target.ID, App: a.App, Source: a.Source, Target: a.Target, SourceHead: src.Head, TargetHead: tgt.Head}
		if err := r.prepare(ctx, tenant, &m, keep); err != nil {
			return nil, problem("", "source", err.Error(), "")
		}
		if len(m.Parts) == 0 {
			return nil, problem("nothing_to_merge", "source", fmt.Sprintf("%s holds nothing %s does not", a.Source, a.Target), "")
		}
		m.settle()
		return []live.NewEvent{{Type: EventCreated, Data: m}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "app.merge.resolve", Source: "apps"}, Title: "Resolve a merge conflict",
		Description: "Resolves one conflict of a merge (forge 19e): the path takes ours (the target's), theirs (the source's), base (the common ancestor's) or a value given (null removes it); the part's candidate is merged again with every resolution it holds.",
		Args:        json.RawMessage(`{"type":"object","properties":{"merge":{"type":"string","description":"The merge's id"},"type":{"type":"string","enum":["app","prefab","graph","entity"],"description":"The part's type"},"id":{"type":"string","description":"The part's id"},"path":{"type":"string","description":"The conflict's path, as the merge lists it"},"take":{"type":"string","enum":["ours","theirs","base","value"],"description":"What the path takes"},"value":{"description":"With take value: the value it takes"}},"required":["merge","type","id","path","take"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"merge":"tasks-m123","type":"entity","id":"order","path":"entity:order.fields.limit.type","take":"theirs"}`)},
		Events:      []string{EventMergeUpdated}, Action: "app.write",
	}, byMerge, func(d live.Decision, m Merge, has bool) ([]live.NewEvent, []live.Problem) {
		if p := need(m, has, MergeConflicts, MergeReady); p != nil {
			return nil, p
		}
		var a struct {
			Type, ID, Path, Take string
			Value                any
		}
		_ = json.Unmarshal(d.Args, &a)
		i := slices.IndexFunc(m.Parts, func(p MergePart) bool { return p.Type == a.Type && p.ID == a.ID })
		if i < 0 || !m.Parts[i].Both {
			return nil, problem("", "id", fmt.Sprintf("the merge does not merge %s %s structurally", a.Type, a.ID), "")
		}
		part := m.Parts[i]
		_, open := part.Resolutions[a.Path]
		if !open && !slices.ContainsFunc(part.Conflicts, func(c diff.Conflict) bool { return c.Path == a.Path }) {
			return nil, problem("", "path", fmt.Sprintf("%s has no conflict at %s", a.Type+" "+a.ID, a.Path), "use a path the merge lists")
		}
		res := map[string]diff.Resolution{}
		for k, v := range part.Resolutions {
			res[k] = v
		}
		res[a.Path] = diff.Resolution{Take: a.Take, Value: a.Value}
		ctx, tenant := d.Context(), d.Principal.TenantID
		again := Merge{App: m.App, Source: m.Source, Target: m.Target, SourceHead: m.SourceHead, TargetHead: m.TargetHead}
		keep := map[string]map[string]diff.Resolution{}
		for _, p := range m.Parts {
			keep[p.Type+"."+p.ID] = p.Resolutions
		}
		keep[a.Type+"."+a.ID] = res
		if err := r.prepare(ctx, tenant, &again, keep); err != nil {
			return nil, problem("", "", err.Error(), "")
		}
		return []live.NewEvent{{Type: EventMergeUpdated, Data: map[string]any{"parts": again.Parts}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "app.merge.approve", Source: "apps"}, Title: "Approve an app merge",
		Description: "Approves a merge (forge 19e) for the part kinds the sender approves among those it changes on the target (entities, pages, access, graphs), as app.approve does a commit. A merge lands once every kind is approved and no conflict is open.",
		Args:        json.RawMessage(`{"type":"object","properties":{"merge":{"type":"string","description":"The merge's id"},"comment":{"type":"string","description":"What the approver says"}},"required":["merge"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"merge":"tasks-m123","comment":"Both changes read well together"}`)},
		Events:      []string{EventApproved}, Action: "app.read",
	}, byMerge, func(d live.Decision, m Merge, has bool) ([]live.NewEvent, []live.Problem) {
		if p := need(m, has, MergeConflicts, MergeReady); p != nil {
			return nil, p
		}
		var a struct{ Comment string }
		_ = json.Unmarshal(d.Args, &a)
		mine := ApproverKinds(d.ServiceArgs, m.App)
		var kinds []string
		for _, k := range m.Kinds {
			if slices.Contains(mine, k) && slices.Contains(m.Waiting, k) {
				kinds = append(kinds, k)
			}
		}
		if len(kinds) == 0 {
			return nil, problem("app_not_approver", "merge", fmt.Sprintf("you approve none of what the merge still needs approved (%s)", strings.Join(m.Waiting, ", ")), "ask an approver of the app")
		}
		return []live.NewEvent{{Type: EventApproved, Data: Approval{Kinds: kinds, Comment: strings.TrimSpace(a.Comment)}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "app.merge.complete", Source: "apps"}, Title: "Land an app merge",
		Description: "Lands an approved merge with no open conflict (forge 19e): in one batch, every candidate becomes the target branch's draft of its part. Refused with merge_stale when either branch committed since the merge was prepared (request it again with again set) and with app_uncommitted when the target has uncommitted changes. Then commit the target with app.commit {app, branch: target, message, merge}, which records the source's head as the commit's second parent and marks the source merged.",
		Args:        json.RawMessage(`{"type":"object","properties":{"merge":{"type":"string","description":"The merge's id"}},"required":["merge"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"merge":"tasks-m123"}`)},
		Events:      []string{EventMergeLanded, live.EventBranched}, Action: "app.write",
	}, byMerge, func(d live.Decision, m Merge, has bool) ([]live.NewEvent, []live.Problem) {
		if p := need(m, has, MergeConflicts, MergeReady); p != nil {
			return nil, p
		}
		switch {
		case m.Open > 0:
			return nil, problem("merge_conflicts", "merge", fmt.Sprintf("%d conflicts are open", m.Open), "resolve them with app.merge.resolve")
		case !m.Approved:
			return nil, problem("merge_not_approved", "merge", fmt.Sprintf("the merge waits for approval of %s", strings.Join(m.Waiting, ", ")), "ask its approvers")
		}
		ctx, tenant := d.Context(), d.Principal.TenantID
		var tgt, src Record
		if ok, err := readInto(d, Projector, branchTarget(m.App, m.Target), &tgt); err != nil || !ok {
			return nil, problem("branch_not_found", "merge", "the target branch is gone", "")
		}
		if ok, err := readInto(d, Projector, branchTarget(m.App, m.Source), &src); err != nil || !ok {
			return nil, problem("branch_not_found", "merge", "the source branch is gone", "")
		}
		if tgt.Head != m.TargetHead || src.Head != m.SourceHead {
			return nil, problem("merge_stale", "merge", "a branch was committed since the merge was prepared", "request the merge again with again: "+m.ID)
		}
		tb := live.Branch{App: m.App, Name: m.Target}
		changes, err := r.Changes(ctx, tenant, tb, tgt.Head)
		if err != nil {
			return nil, problem("", "", err.Error(), "")
		}
		if len(changes) > 0 {
			return nil, problem("app_uncommitted", "merge", fmt.Sprintf("%s has uncommitted changes", m.Target), "commit or discard them first")
		}
		events := []live.NewEvent{}
		for _, p := range m.Parts {
			draft := live.Target{Area: live.AreaDefinitions, Type: p.Type, ID: p.ID}.OnBranch(tb)
			state := p.Candidate
			if p.Removed {
				if tb.IsMain() {
					return nil, problem("merge_removes_on_main", "merge", fmt.Sprintf("the merge removes %s %s, which main removes only by its own command", p.Type, p.ID), "delete it on main with its delete command, commit, and request the merge again")
				}
				state = live.Removed
			}
			events = append(events, live.NewEvent{Type: live.EventBranched, Target: &draft, Data: live.Branched{State: state}})
		}
		return append(events, live.NewEvent{Type: EventMergeLanded, Data: map[string]any{}}), nil
	})
}

// readInto reads a target's projected value through a decision.
func readInto(d live.Decision, projector string, t live.Target, v any) (bool, error) {
	raw, err := d.Read(projector, t)
	if err != nil || raw == nil {
		return false, err
	}
	return true, json.Unmarshal(raw, v)
}
