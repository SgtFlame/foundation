package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
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
	ref   QueryRef
	data  any
	pos   live.Position
	ready chan struct{}
	once  sync.Once
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
	opened    bool
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
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
		queries: map[string]*heldQuery{}, ctx: ctx, cancel: cancel, o: o}
	if rec != nil {
		s.state, s.rev = rec.State, rec.Rev
		if s.state == nil {
			s.state = map[string]any{}
		}
	}
	refs := map[string]QueryRef{}
	if page.Queries != nil {
		refs = page.Queries(params)
	}
	// Every page holds its overlay: the nodes commands added to it
	if o.Subs != nil {
		refs[overlayQuery] = QueryRef{Query: "ui.overlay", Params: map[string]string{"page": page.ID}}
	}
	for name, ref := range refs {
		q := &heldQuery{ref: ref, ready: make(chan struct{})}
		s.queries[name] = q
		raw, _ := json.Marshal(ref.Params)
		if fail := o.Subs.Open(subscribe.OpenArgs{ServiceArgs: args, Query: ref.Query, Params: raw, Sink: &querySink{s: s, name: name, q: q}}); fail != nil {
			cancel()
			return nil, fail
		}
	}
	if rec == nil && page.OnOpen != nil {
		h := &Handle{ServiceArgs: args, Params: params, s: s, request: newID()}
		if err := page.OnOpen(h); err != nil {
			cancel()
			return nil, args.Fail(http.StatusInternalServerError, Log.Wrapf(err, "%s could not open", page.ID))
		}
		s.changed = nil
	}
	deadline := time.After(ready)
	for _, q := range s.queries {
		select {
		case <-q.ready:
		case <-deadline:
		case <-ctx.Done():
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tree = s.renderRoot()
	s.changed = nil
	s.opened = true
	s.save()
	s.sendTree(false)
	return s, nil
}

// ID is the session's id.
func (s *Session) ID() string { return s.id }

// Page is the session's page.
func (s *Session) Page() Page { return s.page }

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
	fn := s.page.Handlers[handler]
	args.Ctx = s.ctx
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
		s.o.Send(ErrorFrame{Type: "ui.error", Session: s.id, Handler: handler, Code: http.StatusNotFound, Message: "this page has no handler " + handler, ErrorCode: "unknown_handler"})
		return
	} else if err != nil {
		f := args.Fail(http.StatusInternalServerError, Log.Wrapf(err, "%s: handler %s failed", s.page.ID, handler))
		s.o.Send(ErrorFrame{Type: "ui.error", Session: s.id, Handler: handler, Code: f.Code, Message: f.Message, ErrorCode: f.ErrorCode})
	}
	s.flush(h.actions)
}

// flush sends what the pending changes patch, with actions.
func (s *Session) flush(actions []Action) {
	patches := s.update()
	if len(patches) == 0 && len(actions) == 0 {
		return
	}
	s.rev++
	s.o.Send(PatchFrame{Type: "ui.patch", Session: s.id, Rev: s.rev, Patches: patches, Actions: actions})
	s.save()
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

func (k *querySink) Snapshot(data any, pos live.Position) {
	k.s.mu.Lock()
	k.q.data, k.q.pos = normalize(data), pos
	k.s.queryChanged(k.name)
	k.s.mu.Unlock()
	k.q.once.Do(func() { close(k.q.ready) })
}

func (k *querySink) Change(delta live.Delta, pos live.Position) {
	k.s.mu.Lock()
	defer k.s.mu.Unlock()
	var value any
	if delta.Value != nil {
		_ = json.Unmarshal(delta.Value, &value)
	}
	switch delta.Op {
	case "replace":
		k.q.data = value
	case "upsert", "insert", "update":
		m, ok := k.q.data.(map[string]any)
		if !ok {
			m = map[string]any{}
		}
		m[delta.Key] = value
		k.q.data = m
	case "remove":
		if m, ok := k.q.data.(map[string]any); ok {
			delete(m, delta.Key)
		}
	}
	k.q.pos = k.q.pos.Merge(pos)
	k.s.queryChanged(k.name)
}

func (k *querySink) Failed(resp features.ServiceResponse) {
	k.q.once.Do(func() { close(k.q.ready) })
	k.s.o.Send(ErrorFrame{Type: "ui.error", Session: k.s.id, Code: resp.Code, Message: resp.Message, ErrorCode: resp.ErrorCode})
}

// queryChanged patches what reads the query, once the page is open.
func (s *Session) queryChanged(name string) {
	s.changed = append(s.changed, "query:"+name)
	if s.opened && !s.closed {
		s.flush(nil)
	}
}

// errNoHandler is a handler that is neither Go nor a graph.
var errNoHandler = errors.New("no such handler")

// renderRoot renders the whole page, with the nodes its overlay adds.
func (s *Session) renderRoot() *Node {
	s.pending = map[*Node]*region{}
	r := &Render{s: s, deps: map[string]bool{}}
	root := s.page.Render(r)
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
	problems := Validate(s.o.Catalog, &shallow)
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
		r := &Render{s: s, deps: map[string]bool{}}
		next := reg.fn(r)
		if next == nil {
			next = N("mui.box", old.Key)
		}
		next.Key = old.Key
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
