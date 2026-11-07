package ui

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
)

// ready is how long opening a page waits for its queries' first snapshots.
const ready = 3 * time.Second

// TreeFrame ← a session's whole tree: when it opens, and when an event
// arrived for a node the browser has but the server no longer does.
type TreeFrame struct {
	Type    string   `json:"type"` // ui.tree
	Session string   `json:"session"`
	Rev     int64    `json:"rev"`
	Tree    *Node    `json:"tree"`
	Title   string   `json:"title,omitempty"`
	Subject *Subject `json:"subject,omitempty"`
	Stale   bool     `json:"stale,omitempty"`
}

// PatchFrame ← the patches and client actions of one change.
type PatchFrame struct {
	Type    string   `json:"type"` // ui.patch
	Session string   `json:"session"`
	Rev     int64    `json:"rev"`
	Patches []Patch  `json:"patches"`
	Actions []Action `json:"actions,omitempty"`
}

// ErrorFrame ← a page that could not open, or a handler that failed.
type ErrorFrame struct {
	Type      string `json:"type"` // ui.error
	Session   string `json:"session"`
	Handler   string `json:"handler,omitempty"`
	Code      uint16 `json:"code"`
	Message   string `json:"message"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// Record is what a session keeps across restarts and instances: everything
// but the tree, which rendering rebuilds.
type Record struct {
	ID       string          `json:"id"`
	UserID   uint64          `json:"userId,string"`
	TenantID uint64          `json:"tenantId,string"`
	Page     string          `json:"page"`
	Params   json.RawMessage `json:"params,omitempty"`
	State    map[string]any  `json:"state"`
	Rev      int64           `json:"rev"`
}

// SessionStore keeps session records.
type SessionStore interface {
	Load(ctx context.Context, tenant uint64, id string) (*Record, error) // nil when there is none
	Save(ctx context.Context, r Record) error
	Delete(ctx context.Context, tenant uint64, id string) error
}

type region struct {
	fn   func(r *Render) *Node
	deps map[string]bool
}

type heldQuery struct {
	ref    QueryRef
	params json.RawMessage // as subscribed, with bound state
	data   any
	pos    live.Position
	ready  chan struct{}
	once   sync.Once
	cancel context.CancelFunc
	failed *features.ServiceResponse // why it ended, when it failed
	owner  *instance                 // the prefab instance holding it, if one does
	into   string                    // a subscription's: the state path its data is kept at
	def    *Prefab                   // a prefab definition's, read once per change
}

// Session is one viewer's open page.
type Session struct {
	mu        sync.Mutex
	id        string
	principal features.Principal
	page      Page
	params    json.RawMessage
	state     map[string]any
	rev       int64
	tree      *Node
	rootDeps  map[string]bool
	regions   map[*Node]*region
	pending   map[*Node]*region
	queries   map[string]*heldQuery
	changed   []string
	instances map[string]*instance // mounted prefab instances by key path
	seen      map[string]bool      // the instances the render under way met
	runs      []lifecycle          // prefab.logic runs for after the tree is sent
	fresh     []*heldQuery         // queries held while rendering
	opened    bool
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
	args      features.ServiceArgs // the opener's, for subscribing again
	o         Options
}

// HandlerRunner runs handlers that are graphs (forge 02-Blueprints): a
// handler id a page has no Go handler for names a published ui.handler
// graph, run at each event in its latest version.
type HandlerRunner interface {
	// RunHandler runs graph id for the event in h; found is false when
	// there is no such handler graph.
	RunHandler(h *Handle, id string) (found bool, err error)
}

// Options are what a session runs with.
type Options struct {
	Catalog   map[string]Component
	Commander Commander
	Graphs    HandlerRunner // nil runs Go handlers only
	Subs      *subscribe.Subscriptions
	Store     SessionStore
	Send      func(frame any) // to the session's live connection
}

// Open opens a page for args.Principal as session id, resuming its view
// state when rec is the stored record, runs OnOpen for a new one, waits for
// its queries and sends the first tree.
func Open(args features.ServiceArgs, id string, page Page, params json.RawMessage, rec *Record, o Options) (*Session, *features.ServiceResponse) {
	ctx, cancel := context.WithCancel(args.Context())
	args.Ctx = ctx
	s := &Session{id: id, principal: args.Principal, page: page, params: params, state: map[string]any{}, regions: map[*Node]*region{},
		queries: map[string]*heldQuery{}, instances: map[string]*instance{}, seen: map[string]bool{}, ctx: ctx, cancel: cancel, args: args, o: o}
	if rec != nil {
		s.state, s.rev = rec.State, rec.Rev
		if s.state == nil {
			s.state = map[string]any{}
		}
	}
	if page.Setup != nil {
		h := &Handle{ServiceArgs: args, Params: params, s: s, request: newID()}
		if err := page.Setup(h); err != nil {
			cancel()
			return nil, args.Fail(http.StatusInternalServerError, Log.Wrapf(err, "%s could not be set up", page.ID))
		}
	}
	refs := map[string]QueryRef{}
	if page.Queries != nil {
		refs = page.Queries(params)
	}
	// Every page holds its overlay: the nodes commands added to it
	if o.Subs != nil && page.prefab == "" {
		refs[overlayQuery] = QueryRef{Query: "ui.overlay", Params: map[string]string{"page": page.ID}}
	}
	// A new session's view state comes first, so queries bound to it
	// open with it
	if rec == nil && page.OnOpen != nil {
		h := &Handle{ServiceArgs: args, Params: params, s: s, request: newID()}
		if err := page.OnOpen(h); err != nil {
			cancel()
			return nil, args.Fail(http.StatusInternalServerError, Log.Wrapf(err, "%s could not open", page.ID))
		}
		s.changed = nil
	}
	// Held under the lock: a query held first may already be delivering
	// its snapshot, which reads the held queries
	s.mu.Lock()
	for name, ref := range refs {
		if fail := s.hold(name, ref); fail != nil {
			s.mu.Unlock()
			cancel()
			return nil, fail
		}
	}
	s.mu.Unlock()
	deadline := time.After(ready)
	wait := func(qs []*heldQuery) {
		for _, q := range qs {
			select {
			case <-q.ready:
			case <-deadline:
			case <-ctx.Done():
			}
		}
	}
	s.mu.Lock()
	waiting := slices.Collect(maps.Values(s.queries))
	s.mu.Unlock()
	wait(waiting)
	// Rendering may hold more queries, prefabs' definitions and their
	// instances' queries: render again once they have their data
	for round := 0; ; round++ {
		s.mu.Lock()
		s.tree = s.renderRoot()
		fresh := s.fresh
		s.fresh = nil
		if len(fresh) == 0 || round == 8 {
			break
		}
		s.mu.Unlock()
		wait(fresh)
	}
	defer s.mu.Unlock()
	s.changed = nil
	s.opened = true
	s.save()
	s.sendTree(false)
	s.settle()
	return s, nil
}

// ID is the session's id.
func (s *Session) ID() string { return s.id }

// Page is the session's page.
func (s *Session) Page() Page { return s.page }

// Queries are the names of the queries the session holds, sorted: its
// page's, its prefabs' definitions (__prefab:<id>) and its instances'
// (<key path>#<name>, <key path>#sub:<state path>).
func (s *Session) Queries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(maps.Keys(s.queries))
}

// Principal is the session's viewer.
func (s *Session) Principal() features.Principal { return s.principal }

// Close ends the session and its subscriptions; forget also removes its
// stored record (the tab was closed, not just disconnected).
func (s *Session) Close(forget bool) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	if forget && s.o.Store != nil {
		_ = s.o.Store.Delete(context.WithoutCancel(s.ctx), s.principal.TenantID, s.id)
	}
}

// Touch stores the record again, so it outlives the store's age while the
// session is open.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.save()
}

func (s *Session) save() {
	if s.o.Store == nil {
		return
	}
	rec := Record{ID: s.id, UserID: s.principal.UserID, TenantID: s.principal.TenantID, Page: s.page.ID, Params: s.params, State: s.state, Rev: s.rev}
	if err := s.o.Store.Save(context.WithoutCancel(s.ctx), rec); err != nil {
		Log.Error(s.ctx, err, "cannot store UI session %s", s.id)
	}
}

func (s *Session) sendTree(stale bool) {
	f := TreeFrame{Type: "ui.tree", Session: s.id, Rev: s.rev, Tree: s.tree, Title: s.page.Title, Stale: stale}
	if s.page.Subject != nil {
		sub := s.page.Subject(s.params)
		f.Subject = &sub
	}
	s.o.Send(f)
}

// Event runs a handler for an event on the node at path, then sends the
// patches. An event for a node the server no longer has gets the whole
// tree back (ui_stale) and runs nothing.
func (s *Session) Event(args features.ServiceArgs, handler string, path []string, payload map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if path != nil && s.tree.At(path) == nil {
		s.sendTree(true)
		return
	}
	args.Ctx = s.ctx
	if kp, name, ok := instanceHandler(handler); ok {
		s.instanceEvent(args, kp, name, payload)
		return
	}
	fn := s.page.Handlers[handler]
	h := &Handle{ServiceArgs: args, Payload: payload, Params: s.params, s: s, request: newID()}
	if fn == nil && s.o.Graphs != nil {
		fn = func(h *Handle) error {
			found, err := s.o.Graphs.RunHandler(h, handler)
			if !found && err == nil {
				err = errNoHandler
			}
			return err
		}
	}
	if fn == nil {
		s.o.Send(ErrorFrame{Type: "ui.error", Session: s.id, Handler: handler, Code: http.StatusNotFound, Message: "this page has no handler " + handler, ErrorCode: "unknown_handler"})
		return
	}
	if err := fn(h); errors.Is(err, errNoHandler) {
		s.handlerFailed(args, handler, err)
		return
	} else if err != nil {
		s.handlerFailed(args, handler, err)
	}
	s.flush(h.actions)
}

// handlerFailed tells the browser a handler is missing or failed.
func (s *Session) handlerFailed(args features.ServiceArgs, handler string, err error) {
	if errors.Is(err, errNoHandler) {
		s.o.Send(ErrorFrame{Type: "ui.error", Session: s.id, Handler: handler, Code: http.StatusNotFound, Message: "this page has no handler " + handler, ErrorCode: "unknown_handler"})
		return
	}
	f := args.Fail(http.StatusInternalServerError, Log.Wrapf(err, "%s: handler %s failed", s.page.ID, handler))
	s.o.Send(ErrorFrame{Type: "ui.error", Session: s.id, Handler: handler, Code: f.Code, Message: f.Message, ErrorCode: f.ErrorCode})
}

// flush sends what the pending changes patch, with actions, then runs the
// prefab.logic runs they caused.
func (s *Session) flush(actions []Action) {
	s.send(actions)
	s.settle()
}

// send sends what the pending changes patch, with actions.
func (s *Session) send(actions []Action) {
	s.fresh = nil
	s.rebind()
	patches := s.update()
	if len(patches) == 0 && len(actions) == 0 {
		return
	}
	s.rev++
	s.o.Send(PatchFrame{Type: "ui.patch", Session: s.id, Rev: s.rev, Patches: patches, Actions: actions})
	s.save()
}

// boundParams are a query's params with its bound view state.
func (s *Session) boundParams(ref QueryRef) json.RawMessage {
	raw, _ := json.Marshal(ref.Params)
	if len(ref.Bind) == 0 {
		return raw
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	for param, src := range ref.Bind {
		if len(src) > 6 && src[:6] == "state." {
			if v := getPath(s.state, src[6:]); v != nil {
				m[param] = v
			}
		}
	}
	raw, _ = json.Marshal(m)
	return raw
}

// hold subscribes to a query for the page, in place of one held under
// the same name.
func (s *Session) hold(name string, ref QueryRef) *features.ServiceResponse {
	if old := s.queries[name]; old != nil && old.cancel != nil {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	q := &heldQuery{ref: ref, params: s.boundParams(ref), ready: make(chan struct{}), cancel: cancel}
	if old := s.queries[name]; old != nil {
		q.data = old.data // shown until the new snapshot replaces it
	}
	s.queries[name] = q
	args := s.args
	args.Ctx = ctx
	if fail := s.o.Subs.Open(subscribe.OpenArgs{ServiceArgs: args, Query: ref.Query, Params: q.params, Sink: &querySink{s: s, name: name, q: q}}); fail != nil {
		cancel()
		return fail
	}
	return nil
}

// rebind subscribes again to the queries whose bound state changed.
func (s *Session) rebind() {
	for name, q := range s.queries {
		if len(q.ref.Bind) == 0 {
			continue
		}
		bound := false
		for _, src := range q.ref.Bind {
			bound = bound || slices.ContainsFunc(s.changed, func(c string) bool { return related(src, c) })
		}
		if !bound || string(s.boundParams(q.ref)) == string(q.params) {
			continue
		}
		if fail := s.hold(name, q.ref); fail != nil {
			s.o.Send(ErrorFrame{Type: "ui.error", Session: s.id, Code: fail.Code, Message: fail.Message, ErrorCode: fail.ErrorCode})
		}
	}
}

type querySink struct {
	s    *Session
	name string
	q    *heldQuery
}

func normalize(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// current reports whether the sink's query is still the one held under
// its name: one replaced by a rebinding is dropped.
func (k *querySink) current() bool { return k.s.queries[k.name] == k.q }

func (k *querySink) Snapshot(data any, pos live.Position) {
	k.s.mu.Lock()
	if !k.current() {
		k.s.mu.Unlock()
		return
	}
	k.q.data, k.q.pos, k.q.def = normalize(data), pos, nil
	k.s.queryChanged(k.name, k.q)
	k.s.mu.Unlock()
	k.q.once.Do(func() { close(k.q.ready) })
}

func (k *querySink) Change(delta live.Delta, pos live.Position) {
	k.s.mu.Lock()
	defer k.s.mu.Unlock()
	if !k.current() {
		return
	}
	k.q.pos = k.q.pos.Merge(pos)
	if delta.Op == live.Reached.Op {
		return
	}
	k.q.data, k.q.def = live.Apply(k.q.data, delta), nil
	k.s.queryChanged(k.name, k.q)
}

func (k *querySink) Failed(resp features.ServiceResponse) {
	// Ready only once the failure is known, so an opening that waits for it
	// renders the problem
	defer k.q.once.Do(func() { close(k.q.ready) })
	k.s.mu.Lock()
	defer k.s.mu.Unlock()
	if !k.current() {
		return
	}
	k.q.failed = &resp
	// A prefab that cannot be read is a problem in place
	if strings.HasPrefix(k.name, definitionQuery) {
		k.s.queryChanged(k.name, k.q)
		return
	}
	k.s.o.Send(ErrorFrame{Type: "ui.error", Session: k.s.id, Code: resp.Code, Message: resp.Message, ErrorCode: resp.ErrorCode})
}

// queryChanged patches what reads the query, once the page is open. A
// subscription's data goes into its state path; an instance's query
// change runs its On query changed logic.
func (s *Session) queryChanged(name string, q *heldQuery) {
	switch {
	case q.into != "":
		setPath(s.state, q.into, q.data)
		s.changed = append(s.changed, "state."+q.into)
	default:
		s.changed = append(s.changed, "query:"+name)
		if q.owner != nil && q.owner.started {
			s.runs = append(s.runs, lifecycle{inst: q.owner, event: onQueryChanged})
		}
	}
	if s.opened && !s.closed {
		s.flush(nil)
	}
}

// errNoHandler is a handler that is neither Go nor a graph.
var errNoHandler = errors.New("no such handler")

// renderRoot renders the whole page, with the nodes its overlay adds.
func (s *Session) renderRoot() *Node {
	s.pending = map[*Node]*region{}
	s.seen = map[string]bool{}
	r := &Render{s: s, deps: map[string]bool{}}
	root := s.page.Render(r)
	s.sweep("")
	if root == nil {
		root = N("mui.box", "root")
	}
	if o := r.Query(overlayQuery); o != nil {
		applyOverlay(root, o)
	}
	s.rootDeps = r.deps
	s.regions = s.pending
	s.pending = nil
	return s.prepare(root)
}

// prepare resolves bound props and puts an error box in place of every
// node that fails validation.
func (s *Session) prepare(n *Node) *Node {
	s.resolve(n)
	if problems := s.check(n); len(problems) > 0 {
		return ErrorNode(n.Key, problems)
	}
	for i, c := range n.Children {
		n.Children[i] = s.prepare(c)
	}
	for _, list := range n.Slots {
		for i, c := range list {
			list[i] = s.prepare(c)
		}
	}
	return n
}

func (s *Session) check(n *Node) []Problem {
	if s.o.Catalog == nil {
		return nil
	}
	shallow := *n
	shallow.Children, shallow.Slots = nil, nil
	problems := validate(s.o.Catalog, &shallow, false)
	c := s.o.Catalog[n.Type]
	if len(n.Children) > 0 && !c.Children {
		problems = append(problems, Problem{Message: n.Type + " takes no children"})
	}
	return problems
}

func (s *Session) resolve(n *Node) {
	for prop, src := range n.Bind {
		if v, ok := s.value(src); ok {
			if n.Props == nil {
				n.Props = map[string]any{}
			}
			n.Props[prop] = v
		}
	}
}

// value reads a binding's source; local ones are the browser's.
func (s *Session) value(src string) (any, bool) {
	switch {
	case len(src) > 6 && src[:6] == "state.":
		return getPath(s.state, src[6:]), true
	case len(src) > 6 && src[:6] == "query:":
		name, path := src[6:], ""
		for i := range name {
			if name[i] == '.' {
				name, path = name[:i], name[i+1:]
				break
			}
		}
		q := s.queries[name]
		if q == nil {
			return nil, true
		}
		return getPath(q.data, path), true
	}
	return nil, false
}

func anyRelated(deps map[string]bool, changed []string) bool {
	for d := range deps {
		for _, c := range changed {
			if related(d, c) {
				return true
			}
		}
	}
	return false
}

// update re-renders the regions whose reads changed and patches the
// bound props outside them.
func (s *Session) update() []Patch {
	changed := s.changed
	s.changed = nil
	if len(changed) == 0 || s.tree == nil {
		return nil
	}
	if anyRelated(s.rootDeps, changed) {
		next := s.renderRoot()
		patches := Diff(nil, s.tree, next)
		s.tree = next
		return patches
	}
	// The regions to render again, outermost first
	paths := map[*Node][]string{}
	s.tree.Walk(func(path []string, n *Node) {
		if s.regions[n] != nil {
			paths[n] = path
		}
	})
	var targets []*Node
	for n, reg := range s.regions {
		if _, ok := paths[n]; ok && anyRelated(reg.deps, changed) {
			targets = append(targets, n)
		}
	}
	slices.SortFunc(targets, func(a, b *Node) int { return len(paths[a]) - len(paths[b]) })
	var done [][]string
	var patches []Patch
	for _, old := range targets {
		path := paths[old]
		if slices.ContainsFunc(done, func(p []string) bool { return isPrefix(p, path) }) {
			continue
		}
		reg := s.regions[old]
		s.pending = map[*Node]*region{}
		s.seen = map[string]bool{}
		r := &Render{s: s, deps: map[string]bool{}}
		next := reg.fn(r)
		s.sweep(kpOf(append([]string{s.tree.Key}, path...)))
		if next == nil {
			next = N("mui.box", old.Key)
		}
		next.Key = old.Key
		if within := s.pending[next]; within != nil { // as Render.Region does
			maps.Copy(r.deps, within.deps)
		}
		s.pending[next] = &region{fn: reg.fn, deps: r.deps}
		next = s.prepare(next)
		old.Walk(func(_ []string, n *Node) { delete(s.regions, n) })
		for n, rg := range s.pending {
			s.regions[n] = rg
		}
		s.pending = nil
		if len(path) == 0 {
			s.tree = next
		} else if parent := s.tree.At(path[:len(path)-1]); parent != nil {
			for i, c := range parent.Children {
				if c == old {
					parent.Children[i] = next
				}
			}
		}
		patches = append(patches, Diff(path, old, next)...)
		done = append(done, path)
	}
	// Bound props elsewhere get props patches alone
	s.tree.Walk(func(path []string, n *Node) {
		if len(n.Bind) == 0 || slices.ContainsFunc(done, func(p []string) bool { return isPrefix(p, path) }) {
			return
		}
		var set map[string]any
		for prop, src := range n.Bind {
			if !slices.ContainsFunc(changed, func(c string) bool { return related(src, c) }) {
				continue
			}
			v, ok := s.value(src)
			if !ok || equalJSON(n.Props[prop], v) {
				continue
			}
			if n.Props == nil {
				n.Props = map[string]any{}
			}
			n.Props[prop] = v
			if set == nil {
				set = map[string]any{}
			}
			set[prop] = v
		}
		if set != nil {
			patches = append(patches, Patch{Op: "props", Path: path, Props: set})
		}
	})
	return patches
}

func isPrefix(prefix, path []string) bool {
	return len(prefix) <= len(path) && slices.Equal(prefix, path[:len(prefix)])
}
