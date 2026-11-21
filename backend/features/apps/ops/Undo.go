package ops

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Deps are what undo needs, made after the registry.
type Deps struct {
	Bus   *bus.Bus
	Store Store
}

// Scope is what an editor's undo reaches (forge 06b): an app (app:<id>,
// every op logged under it), its manifest (app:<id>/manifest), or one
// prefab, graph or entity type (prefab:<id>, graph:<id>, entity:<id>). A
// request is in a scope when one of its ops is.
type Scope struct {
	Kind string `json:"kind"` // app, manifest, prefab, graph or entity
	ID   string `json:"id"`
}

// ParseScope reads a scope as the browser names it.
func ParseScope(s string) (Scope, error) {
	kind, id, _ := strings.Cut(s, ":")
	if kind == "app" {
		if app, ok := strings.CutSuffix(id, "/manifest"); ok {
			kind, id = "manifest", app
		}
	}
	if !slices.Contains([]string{"app", "manifest", "prefab", "graph", "entity"}, kind) || !live.ValidToken(id) {
		return Scope{}, log.Code("invalid_scope").Errorf("%q is not a scope: use app:<id>, app:<id>/manifest, prefab:<id>, graph:<id> or entity:<id>", s)
	}
	return Scope{Kind: kind, ID: id}, nil
}

// Has reports whether an op is in the scope.
func (s Scope) Has(e Entry) bool {
	switch s.Kind {
	case "app":
		return e.App == s.ID
	case "manifest":
		return e.TargetType == "app" && e.Target == s.ID
	case "graph":
		return e.TargetType == drafts.TargetType && e.Target == s.ID
	}
	return e.TargetType == s.Kind && e.Target == s.ID
}

// StackItem is one request an undo or redo would reverse.
type StackItem struct {
	Request string    `json:"request"`
	Summary string    `json:"summary"` // for a redo, what it brings back
	Actor   string    `json:"actor,omitempty"`
	Time    time.Time `json:"time"`
	Ops     int       `json:"ops"`
}

// Stack is the ops.stack query's result: the caller's undoable and
// redoable requests in a scope, newest first.
type Stack struct {
	Undo []StackItem `json:"undo"`
	Redo []StackItem `json:"redo"`
}

// Requests are entries grouped by request, oldest first, each with its
// ops oldest first.
func Requests(entries []Entry) []Request {
	l := group(entries, nil)
	slices.Reverse(l.Requests)
	return l.Requests
}

func undoesOf(r Request) string {
	if len(r.Ops) == 0 {
		return ""
	}
	return r.Ops[0].Undoes
}

// cancelled are the requests whose effect is gone (gone): each undone by a
// later request that is not undone in turn (undone), and that undo with it.
// Their ops are no one's change any more, so they conflict with nothing.
func cancelled(all []Request) (gone, undone map[string]bool) {
	gone, undone = map[string]bool{}, map[string]bool{}
	for i := len(all) - 1; i >= 0; i-- {
		r := all[i]
		if u := undoesOf(r); u != "" && !undone[r.ID] {
			undone[u] = true
			gone[u], gone[r.ID] = true, true
		}
	}
	return gone, undone
}

// Titles names commands for summaries.
type Titles func(command string) string

// describe is one op in a few words: its command's title, its target and
// the item it changed (Add a page (tasks /mine)).
func describe(e Entry, title Titles) string {
	for _, it := range Items(e) {
		if it.Key != "" {
			return fmt.Sprintf("%s (%s %s)", title(e.Command), e.Target, it.Key)
		}
	}
	return fmt.Sprintf("%s (%s)", title(e.Command), e.Target)
}

// summarize is a request in a few words: its first op and how many more.
func summarize(r Request, title Titles) string {
	if len(r.Ops) == 0 {
		return r.ID
	}
	s := describe(r.Ops[0], title)
	if n := len(r.Ops) - 1; n == 1 {
		s += " and 1 more change"
	} else if n > 1 {
		s += fmt.Sprintf(" and %d more changes", n)
	}
	return s
}

// StackOf is a principal's stacks in a scope, from every request in the
// log, oldest first. A request of theirs is pushed on the undo stack and
// empties the redo stack; undoing one moves it to the redo stack as its
// undo, and undoing that undo (a redo) moves it back.
func StackOf(all []Request, scope Scope, principal uint64, title Titles) Stack {
	byID := map[string]Request{}
	for _, r := range all {
		byID[r.ID] = r
	}
	var undo, redo []string // oldest first
	for _, r := range all {
		if r.Principal != principal || !slices.ContainsFunc(r.Ops, scope.Has) {
			continue
		}
		u := undoesOf(r)
		switch {
		case u != "" && slices.Contains(undo, u):
			undo = slices.DeleteFunc(undo, func(x string) bool { return x == u })
			redo = append(redo, r.ID)
		case u != "" && slices.Contains(redo, u):
			redo = slices.DeleteFunc(redo, func(x string) bool { return x == u })
			undo = append(undo, r.ID)
		default:
			undo = append(undo, r.ID)
			redo = nil
		}
	}
	// item is a request on a stack; a redo names what it brings back,
	// which the undo it reverses undid
	item := func(id string, redoing bool) StackItem {
		r := byID[id]
		shown := r
		for redoing || undoesOf(shown) != "" {
			o, ok := byID[undoesOf(shown)]
			if !ok {
				break
			}
			shown, redoing = o, !redoing
		}
		return StackItem{Request: id, Summary: summarize(shown, title), Actor: r.Actor, Time: r.Time, Ops: len(r.Ops)}
	}
	s := Stack{Undo: []StackItem{}, Redo: []StackItem{}}
	for i := len(undo) - 1; i >= 0; i-- {
		s.Undo = append(s.Undo, item(undo[i], false))
	}
	for i := len(redo) - 1; i >= 0; i-- {
		s.Redo = append(s.Redo, item(redo[i], true))
	}
	return s
}

// Conflict is an item of the request being undone that someone else
// changed after it (forge 06b question 2).
type Conflict struct {
	Item      Item      `json:"item"`
	Command   string    `json:"command"` // the later change
	Request   string    `json:"request"`
	Principal uint64    `json:"principal,string"`
	Actor     string    `json:"actor,omitempty"`
	Time      time.Time `json:"time"`
}

// Skipped is an op an undo leaves: irreversible, or (forced) conflicting.
type Skipped struct {
	Command string `json:"command"`
	Target  string `json:"target"`
	Why     string `json:"why"`
}

// Plan is what undoing a request does.
type Plan struct {
	Request   string     `json:"request"` // the request undone
	Undoes    string     `json:"undoes,omitempty"`
	App       string     `json:"app,omitempty"`
	Summary   string     `json:"summary"`
	Ops       []live.Op  `json:"ops"`     // the inverses sent, in order
	Changes   []string   `json:"changes"` // each op taken back, in words, newest first
	Conflicts []Conflict `json:"conflicts"`
	Skipped   []Skipped  `json:"skipped"`
}

// PlanUndo is the inverses that undo a request, newest op first, and the
// conflicts with what others changed since: with force, a conflicting op
// is skipped and the rest undone; without, the conflicts are the answer.
func PlanUndo(all []Request, request string, force bool, title Titles) (Plan, error) {
	i := slices.IndexFunc(all, func(r Request) bool { return r.ID == request })
	if i < 0 {
		return Plan{}, log.Code("request_not_found").Errorf("there is no request %q in the log", request)
	}
	r := all[i]
	gone, undone := cancelled(all)
	if undone[r.ID] {
		return Plan{}, log.Code("request_undone").Errorf("the request %q is undone already", request)
	}
	p := Plan{Request: r.ID, App: r.App, Summary: summarize(r, title), Ops: []live.Op{}, Changes: []string{}, Conflicts: []Conflict{}, Skipped: []Skipped{}}
	var later []Entry
	for _, o := range all[i+1:] {
		if !gone[o.ID] {
			later = append(later, o.Ops...)
		}
	}
	for j := len(r.Ops) - 1; j >= 0; j-- {
		e := r.Ops[j]
		if e.Irreversible != "" {
			p.Skipped = append(p.Skipped, Skipped{Command: e.Command, Target: e.Target, Why: e.Irreversible})
			continue
		}
		var conflicts []Conflict
		items := Items(e)
		for _, l := range later {
			for _, it := range items {
				if slices.ContainsFunc(Items(l), it.Overlaps) {
					conflicts = append(conflicts, Conflict{Item: it, Command: l.Command, Request: l.Request, Principal: l.Principal, Actor: l.Actor, Time: l.Time})
					break
				}
			}
		}
		if len(conflicts) > 0 {
			p.Conflicts = append(p.Conflicts, conflicts...)
			p.Skipped = append(p.Skipped, Skipped{Command: e.Command, Target: e.Target, Why: "changed since by someone else"})
			continue
		}
		p.Ops = append(p.Ops, e.Inverse...)
		p.Changes = append(p.Changes, describe(e, title))
	}
	if !force && len(p.Conflicts) > 0 {
		p.Ops, p.Changes = []live.Op{}, []string{}
	}
	return p, nil
}

// all is every request in the tenant's log, oldest first, once the log has
// folded every definition event.
func all(args features.ServiceArgs, store Store) ([]Request, error) {
	entries, err := Entries(args, store, "")
	if err != nil {
		return nil, err
	}
	return Requests(entries), nil
}

// UndoArgs are ops.undo's and ops.redo's arguments.
type UndoArgs struct {
	Scope   string `json:"scope,omitempty"`
	Request string `json:"request,omitempty"`
	Force   bool   `json:"force,omitempty"`
	DryRun  bool   `json:"dryRun,omitempty"`
}

// UndoResult is what an undo or redo did, or would do. A redo's changes
// are the undo's ops it takes back.
type UndoResult struct {
	// Status is done, preview (dryRun), conflicts (refused: force undoes
	// the rest) or nothing (nothing to undo).
	Status string `json:"status"`
	Plan
	Sent string `json:"sent,omitempty"` // the undo's own request, when done
	Seq  uint64 `json:"seq,omitempty"`
}

// Undo undoes the caller's last request in the scope, or the one named,
// as one new request committed atomically; redo undoes their last undo.
// With dryRun it says what it would do, deciding every inverse without
// committing, so a refusal shows before the user confirms.
func Undo(args features.ServiceArgs, deps Deps, a UndoArgs, redo bool, actor string) (UndoResult, *features.ServiceResponse) {
	if a.Scope == "" && a.Request == "" {
		return UndoResult{}, args.Fail(http.StatusBadRequest, log.Code("invalid_args").New("name a scope or a request"))
	}
	reqs, err := all(args, deps.Store)
	if err != nil {
		return UndoResult{}, args.Fail(http.StatusInternalServerError, err)
	}
	title := titles(deps.Store.Registry())
	request := a.Request
	if request == "" {
		scope, err := ParseScope(a.Scope)
		if err != nil {
			return UndoResult{}, args.Fail(http.StatusBadRequest, err)
		}
		s := StackOf(reqs, scope, args.Principal.UserID, title)
		list := s.Undo
		if redo {
			list = s.Redo
		}
		if len(list) == 0 {
			return UndoResult{Status: "nothing", Plan: Plan{Ops: []live.Op{}, Changes: []string{}, Conflicts: []Conflict{}, Skipped: []Skipped{}}}, nil
		}
		request = list[0].Request
	} else if redo {
		// A redo names an undo, or the request it undid: the undo is undone
		_, undone := cancelled(reqs)
		if i := slices.IndexFunc(reqs, func(r Request) bool { return r.ID == request }); i >= 0 && undoesOf(reqs[i]) == "" && undone[request] {
			for _, r := range slices.Backward(reqs) {
				if undoesOf(r) == request {
					request = r.ID
					break
				}
			}
		}
	}
	p, err := PlanUndo(reqs, request, a.Force, title)
	if err != nil {
		return UndoResult{}, args.Fail(http.StatusUnprocessableEntity, err)
	}
	p.Undoes = request
	if redo {
		// What a redo brings back is what the undo undid
		if i := slices.IndexFunc(reqs, func(r Request) bool { return r.ID == request }); i >= 0 {
			if j := slices.IndexFunc(reqs, func(r Request) bool { return r.ID == undoesOf(reqs[i]) }); j >= 0 {
				p.Summary = summarize(reqs[j], title)
			}
		}
	}
	res := UndoResult{Status: "done", Plan: p}
	if len(p.Conflicts) > 0 && !a.Force {
		res.Status = "conflicts"
		return res, nil
	}
	if len(p.Ops) == 0 {
		res.Status = "nothing"
		return res, nil
	}
	id := fmt.Sprintf("undo-%d", features.NewTSID())
	if redo {
		id = fmt.Sprintf("redo-%d", features.NewTSID())
	}
	t := deps.Bus.Transact(bus.Request{ServiceArgs: args, ID: id, RequestID: id, App: p.App, Actor: actor, Undoes: request}, slices.Clone(p.Ops), a.DryRun)
	if t.Common.Code >= 300 {
		resp := t.Common
		if len(t.Problems) > 0 {
			resp.Message = t.Problems[0].Message
		}
		return res, &resp
	}
	if a.DryRun {
		res.Status = "preview"
		return res, nil
	}
	res.Sent, res.Seq = id, t.Seq
	return res, nil
}

// titles names commands by their registered titles.
func titles(reg *extensions.Registry) Titles {
	known := map[string]string{}
	for _, c := range live.Commands.All(reg) {
		known[c.ID] = c.Title
	}
	return func(command string) string { return cmp.Or(known[command], command) }
}

// storeTitles names commands by the titles registered with a query's
// store, or by their types when it has no registry.
func storeTitles(store live.Store) Titles {
	if s, ok := store.(interface{ Registry() *extensions.Registry }); ok {
		return titles(s.Registry())
	}
	return func(c string) string { return c }
}

// StackParams are ops.stack's parameters.
type StackParams struct {
	Scope string `json:"scope"`
}

func runStack(f live.Feed) error {
	var q StackParams
	if err := json.Unmarshal(f.Params(), &q); err != nil {
		return log.Code("invalid_params").Wrap(err)
	}
	scope, err := ParseScope(q.Scope)
	if err != nil {
		return err
	}
	args := f.Args()
	entries, err := f.Store().Watch(f.Context(), args.Principal.TenantID, Projector, ">")
	if err != nil {
		return err
	}
	title := storeTitles(f.Store())
	var list []Entry
	pos := live.Position{}
	started := false
	var last string
	send := func() {
		slices.SortFunc(list, func(a, b Entry) int { return cmp.Compare(a.Seq, b.Seq) })
		s := StackOf(Requests(list), scope, args.Principal.UserID, title)
		raw, _ := json.Marshal(s)
		switch {
		case !started:
			started = true
			f.Snapshot(s, pos)
		case string(raw) != last:
			f.Change(live.Delta{Op: "replace", Value: raw}, pos)
		default:
			f.Change(live.Reached, pos)
		}
		last = string(raw)
	}
	for e := range entries {
		if e == nil {
			send()
			continue
		}
		pos = pos.Merge(e.Value.Position())
		var x Entry
		if !e.Deleted && json.Unmarshal(e.Value.Value, &x) == nil {
			list = append(list, x)
		}
		if started {
			send()
		}
	}
	return nil
}

// Status is the ops.status query's result: what a request changed and
// whether it is undone now, for the chat's Undo and Redo on a round.
type Status struct {
	Request  string `json:"request"`
	Ops      int    `json:"ops"`      // its ops in the log
	Undoable int    `json:"undoable"` // those with an inverse
	Undone   bool   `json:"undone"`
	UndoneBy string `json:"undoneBy,omitempty"` // the undo that undid it, which a redo undoes
}

// StatusOf is a request's status among every request in the log.
func StatusOf(all []Request, request string) Status {
	s := Status{Request: request}
	_, undone := cancelled(all)
	s.Undone = undone[request]
	for _, r := range all {
		if r.ID == request {
			s.Ops = len(r.Ops)
			for _, e := range r.Ops {
				if e.Irreversible == "" {
					s.Undoable++
				}
			}
		}
		if s.Undone && undoesOf(r) == request && !undone[r.ID] {
			s.UndoneBy = r.ID
		}
	}
	return s
}

func runStatus(f live.Feed) error {
	var q struct {
		Request string `json:"request"`
	}
	if err := json.Unmarshal(f.Params(), &q); err != nil || q.Request == "" {
		return log.Code("invalid_params").New("name a request")
	}
	entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, Projector, ">")
	if err != nil {
		return err
	}
	may := readable(f.Args())
	var list []Entry
	pos := live.Position{}
	started := false
	var last string
	send := func() {
		slices.SortFunc(list, func(a, b Entry) int { return cmp.Compare(a.Seq, b.Seq) })
		s := StatusOf(Requests(list), q.Request)
		raw, _ := json.Marshal(s)
		switch {
		case !started:
			started = true
			f.Snapshot(s, pos)
		case string(raw) != last:
			f.Change(live.Delta{Op: "replace", Value: raw}, pos)
		default:
			f.Change(live.Reached, pos)
		}
		last = string(raw)
	}
	for e := range entries {
		if e == nil {
			send()
			continue
		}
		pos = pos.Merge(e.Value.Position())
		var x Entry
		if !e.Deleted && json.Unmarshal(e.Value.Value, &x) == nil && may(x) {
			list = append(list, x)
		}
		if started {
			send()
		}
	}
	return nil
}

// ContributeUndo registers ops.stack, ops.status, the ops.undo and ops.redo calls, and
// their tools; deps returns the bus and the projections once made.
func ContributeUndo(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "ops.stack", Source: "apps"}, Title: "Undo and redo",
		Description: "The caller's undoable and redoable requests in a scope (app:<id>, app:<id>/manifest, prefab:<id>, graph:<id>, entity:<id>), newest first: {undo: [{request, summary, actor, time, ops}], redo: [...]}. A request is in a scope when one of its ops is; a new request empties the redo list of its scopes.",
		Params:      json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string"}},"required":["scope"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"scope":"prefab:taskRow"}`)},
		Action:      "app.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return apps.Resource(p, "") },
		Run:         runStack,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "ops.status", Source: "apps"}, Title: "A request's status",
		Description: "What a request changed and whether it is undone now: {request, ops, undoable, undone, undoneBy}. The chat shows Undo or Redo on a round (request round-<roundId>) by it.",
		Params:      json.RawMessage(`{"type":"object","properties":{"request":{"type":"string"}},"required":["request"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"request":"round-42"}`)},
		Action:      "app.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return apps.Resource(p, "") },
		Run:         runStatus,
	})
	for _, redo := range []bool{false, true} {
		id, title, description := "ops.undo", "Undo", "Undoes a request: the caller's last in scope, or the one named by request (any in the log), sending its ops' inverses, newest first, as one new request committed atomically. A change someone else made since to an item it changed is a conflict: the undo is refused naming them, unless force, which undoes the rest and leaves those items as they are. Publishing and installs are not undone and are named as skipped. dryRun says what it would do, without doing it."
		if redo {
			id, title, description = "ops.redo", "Redo", "Redoes what an undo undid: the caller's last undo in scope, or the undo named by request (or the request it undid), by undoing the undo. Conflicts, force and dryRun as for ops.undo."
		}
		redo := redo
		live.Calls.Register(reg, live.Call{
			Descriptor: extensions.Descriptor{ID: id, Source: "apps"}, Title: title, Description: description + " Ends with one result event.",
			Run: func(c live.CallRun) *features.ServiceResponse {
				var a UndoArgs
				if err := json.Unmarshal(c.Args, &a); err != nil {
					return c.Fail(http.StatusBadRequest, log.Code("invalid_args").New(err.Error()))
				}
				res, fail := Undo(c.ServiceArgs, deps(), a, redo, "")
				if fail != nil {
					return fail
				}
				c.Emit("result", res)
				return nil
			},
		})
		tools.Tools.Register(reg, tools.Tool{
			Descriptor: extensions.Descriptor{ID: id, Source: "apps"}, Title: title, Effect: tools.EffectWrite,
			Description: description + " To undo this chat's own last round, name its request (round-<roundId>). Ask the user before force.",
			Input:       json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","description":"app:<id>, app:<id>/manifest, prefab:<id>, graph:<id> or entity:<id>"},"request":{"type":"string"},"force":{"type":"boolean"},"dryRun":{"type":"boolean"}}}`),
			Run: func(call tools.Call) (tools.Result, error) {
				a, err := tools.Decode[UndoArgs](call)
				if err != nil {
					return tools.Result{}, err
				}
				res, fail := Undo(call.ServiceArgs, deps(), a, redo, "chat")
				if fail != nil {
					return tools.Result{}, fmt.Errorf("%s", fail.Message)
				}
				raw, _ := json.Marshal(res)
				var content map[string]any
				_ = json.Unmarshal(raw, &content)
				return tools.Result{Content: content, Summary: title + ": " + res.Status + ", " + res.Summary}, nil
			},
		})
	}
}
