package live

import (
	"context"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// OnBranch is a store reading the parts of branch b's app as the branch
// has them (forge 19a): a branchable projection's key is the part's draft
// on the branch when it has one, else the branch's base's, and a part the
// branch removed is not there. Keys stay the parts' own (prefab.orderRow),
// so a query reads a branch as it reads main. Other projections, and
// main, read as the store does.
func OnBranch(store Store, reg *extensions.Registry, b Branch) Store {
	if b.IsMain() {
		return store
	}
	hook, ok := BranchingOf(reg)
	if !ok {
		return store
	}
	types := map[string]map[string]bool{}
	for _, pr := range Projectors.All(reg) {
		if pr.Branchable {
			types[pr.ID] = map[string]bool{}
			for _, t := range pr.Types {
				types[pr.ID][t] = true
			}
		}
	}
	return &branchStore{store: store, hook: hook, b: b, types: types, reg: reg}
}

// drafts watches with every branch's drafts, when the store can.
type drafts interface {
	WatchDrafts(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *Entry, error)
}

type branchStore struct {
	store Store
	hook  Branching
	b     Branch
	types map[string]map[string]bool // branchable projector → its target types
	reg   *extensions.Registry
}

// Registry is the registry the store reads with.
func (s *branchStore) Registry() *extensions.Registry { return s.reg }

// part is the target a branchable projection's key names, ok false for a
// key the branch does not overlay.
func (s *branchStore) part(projector, key string) (Target, bool) {
	typ, id, found := strings.Cut(key, ".")
	if !found || !s.types[projector][typ] {
		return Target{}, false
	}
	return Target{Area: AreaDefinitions, Type: typ, ID: id}, true
}

func (s *branchStore) Get(ctx context.Context, tenant uint64, projector, key string) (Projected, bool, error) {
	t, ok := s.part(projector, key)
	if !ok || IsBranchID(t.ID) {
		return s.store.Get(ctx, tenant, projector, key)
	}
	v, ok, err := s.store.Get(ctx, tenant, projector, t.OnBranch(s.b).Key())
	if err != nil {
		return v, false, err
	}
	if ok && IsRemoved(v.Value) {
		return Projected{}, false, nil
	}
	if ok && v.Value != nil {
		return v, true, nil
	}
	return s.hook.Base(ctx, tenant, s.b, projector, t)
}

// Watch sends the keys matching pattern as the branch has them, each under
// the part's own key: a change to a part's draft on the branch, or on main
// where the branch has none, sends its value again.
func (s *branchStore) Watch(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *Entry, error) {
	if s.types[projector] == nil {
		return s.store.Watch(ctx, tenant, projector, pattern)
	}
	watch := pattern
	if t, ok := s.part(projector, pattern); ok && !strings.ContainsAny(t.ID, "*>") {
		// One part: its draft here and on main (a type's keys hold both)
		watch = t.Type + ".*"
	}
	var in <-chan *Entry
	var err error
	if d, ok := s.store.(drafts); ok {
		in, err = d.WatchDrafts(ctx, tenant, projector, watch)
	} else {
		in, err = s.store.Watch(ctx, tenant, projector, watch)
	}
	if err != nil {
		return nil, err
	}
	out := make(chan *Entry, 16)
	go func() {
		defer close(out)
		sent := map[string]bool{} // the keys sent with a value
		initial := true
		emit := func(key string) bool {
			if !keyMatches(pattern, key) {
				return true
			}
			v, ok, err := s.Get(ctx, tenant, projector, key)
			if err != nil {
				return true
			}
			var e *Entry
			switch {
			case ok:
				e = &Entry{Key: key, Value: v, Initial: initial}
				sent[key] = true
			case sent[key]:
				e = &Entry{Key: key, Deleted: true, Initial: initial}
				delete(sent, key)
			default:
				return true
			}
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for e := range in {
			if e == nil {
				initial = false
				select {
				case out <- nil:
				case <-ctx.Done():
					return
				}
				continue
			}
			typ, id, _ := strings.Cut(e.Key, ".")
			part, b := SplitBranchID(id)
			if !b.IsMain() && b != s.b {
				continue // another branch's draft
			}
			if !emit(typ + "." + part) {
				return
			}
		}
	}()
	return out, nil
}

// keyMatches reports whether a key matches a KV key pattern: "*" is one
// token, ">" the rest.
func keyMatches(pattern, key string) bool {
	if pattern == ">" {
		return true
	}
	ps, ks := strings.Split(pattern, "."), strings.Split(key, ".")
	for i, p := range ps {
		if p == ">" {
			return len(ks) > i
		}
		if i >= len(ks) || (p != "*" && p != ks[i]) {
			return false
		}
	}
	return len(ps) == len(ks)
}
