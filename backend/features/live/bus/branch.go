package bus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/live"
)

// branching is one decision's view of a branch (forge 19a): parts of the
// app's branchable types are read from their drafts on the branch, else
// from the branch's base, and changed on their drafts there, the first
// change copying the base's state in.
type branching struct {
	b      *Bus
	ctx    context.Context
	tenant uint64
	br     live.Branch
	hook   live.Branching
	types  map[string]string // branchable target type → its projector
	// copies are the parts read from the base, by their draft's key:
	// a change to one commits a branched event before its own
	copies map[string]copied
	// checked are the drafts the branch may change, by key
	checked map[string]bool
	// states are the parts' states as read, by their logical key
	states map[string]json.RawMessage
}

type copied struct {
	from  uint64
	state json.RawMessage
}

// newBranching is a request's branching, nil on main. It refuses a
// branch without its app, with an invalid name, or a command that does
// not change an app's part.
func (b *Bus) newBranching(ctx context.Context, tenant uint64, r Request, cmd live.Command, tg live.Target) (*branching, *live.Problem) {
	br := live.Branch{App: r.App, Name: r.Branch}
	if br.IsMain() {
		return nil, nil
	}
	switch {
	case r.App == "":
		return nil, &live.Problem{Path: "branch", Message: "a branch is an app's: name the app the request is for", Fix: "send app with branch"}
	case !live.ValidBranchName(r.Branch):
		return nil, &live.Problem{Path: "branch", Message: fmt.Sprintf("%q is not a branch name", r.Branch), Fix: "lower-case letters, digits and '-'"}
	case cmd.NoBranch:
		return nil, &live.Problem{Code: "not_on_branch", Path: "branch", Message: fmt.Sprintf("%s is not sent on a branch", cmd.ID), Fix: "merge the branch into main and send it there"}
	}
	hook, ok := live.BranchingOf(b.p.Registry())
	if !ok {
		return nil, &live.Problem{Path: "branch", Message: "this server keeps no branches"}
	}
	x := &branching{b: b, ctx: ctx, tenant: tenant, br: br, hook: hook, types: live.BranchableTypes(b.p.Registry()), copies: map[string]copied{}, checked: map[string]bool{}, states: map[string]json.RawMessage{}}
	if !x.branched(tg) {
		return nil, &live.Problem{Code: "not_on_branch", Path: "branch", Message: fmt.Sprintf("%s does not change an app's part, so it is not sent on a branch", cmd.ID)}
	}
	return x, nil
}

// branched reports whether t is drafted per branch.
func (x *branching) branched(t live.Target) bool {
	if x == nil || t.Area != live.AreaDefinitions {
		return false
	}
	_, ok := x.types[t.Type]
	return ok
}

// physical is where t's events go: its draft on the branch.
func (x *branching) physical(t live.Target) live.Target {
	if !x.branched(t) {
		return t
	}
	return t.OnBranch(x.br)
}

// read is t's state in projector on the branch, as the decision sees it:
// the draft's when it has one, else the base's (noted as a copy for the
// draft's key). seq is the draft's own sequence, 0 when it has none.
func (x *branching) read(projector string, t live.Target, wait uint64) (json.RawMessage, uint64, error) {
	pt := x.physical(t)
	v, err := x.b.p.WaitFor(x.ctx, x.tenant, projector, pt.Key(), wait, catchUp)
	if err != nil {
		return nil, 0, err
	}
	if live.IsRemoved(v.Value) {
		return nil, v.Seq, nil
	}
	if v.Value != nil || !x.branched(t) || projector != x.types[t.Type] {
		if projector == x.types[t.Type] {
			x.states[t.Key()] = v.Value
		}
		return v.Value, v.Seq, nil
	}
	base, ok, err := x.hook.Base(x.ctx, x.tenant, x.br, projector, t)
	if err != nil {
		return nil, 0, err
	}
	if !ok || base.Value == nil {
		return nil, v.Seq, nil
	}
	x.copies[pt.Key()] = copied{from: base.Seq, state: base.Value}
	x.states[t.Key()] = base.Value
	return base.Value, v.Seq, nil
}

// check refuses changing t on the branch: the hook's rules, read once per
// draft.
func (x *branching) check(t live.Target, state json.RawMessage) *live.Problem {
	if !x.branched(t) {
		return nil
	}
	pt := x.physical(t)
	if x.checked[pt.Key()] {
		return nil
	}
	if x.hook.Check != nil {
		if p := x.hook.Check(x.ctx, x.tenant, x.br, x.types[t.Type], t, state); p != nil {
			return p
		}
	}
	x.checked[pt.Key()] = true
	return nil
}

// events puts a decision's events on the branch: each on its target's
// draft, preceded by a branched event for a draft its first change
// copies, and reports the index of the first of the command's own events
// (the op rides on it). own is the command's logical target.
func (x *branching) events(own live.Target, events []live.NewEvent) ([]live.NewEvent, int, *live.Problem) {
	out := make([]live.NewEvent, 0, len(events)+1)
	var copiesFirst []live.NewEvent
	seen := map[string]bool{}
	for _, ne := range events {
		t := own
		if ne.Target != nil {
			t = *ne.Target
		}
		if x.branched(t) {
			if _, read := x.states[t.Key()]; !read && t != own {
				if _, _, err := x.read(x.types[t.Type], t, 0); err != nil {
					return nil, 0, &live.Problem{Message: err.Error()}
				}
			}
			if p := x.check(t, x.states[t.Key()]); p != nil {
				return nil, 0, p
			}
		}
		pt := x.physical(t)
		if c, ok := x.copies[pt.Key()]; ok && !seen[pt.Key()] {
			target := pt
			copiesFirst = append(copiesFirst, live.NewEvent{Type: live.EventBranched, Data: live.Branched{From: c.from, State: c.state}, Target: &target})
		}
		seen[pt.Key()] = true
		ne.Target = &pt
		out = append(out, ne)
	}
	return append(copiesFirst, out...), len(copiesFirst), nil
}

// name is the branch's name, "" on main.
func (x *branching) name() string {
	if x == nil {
		return ""
	}
	return x.br.Name
}
