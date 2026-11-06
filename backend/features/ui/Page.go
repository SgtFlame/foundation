package ui

import (
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Apps is the ui.app point: the installed applications. Until 05-Applications
// they are contributed in Go, as the Hello app is.
var Apps = extensions.NewPoint[App]("ui.app", "Applications rendered by the server inside the workbench")

// Pages is the ui.page point: pages an application can open.
var Pages = extensions.NewPoint[Page]("ui.page", "Server-rendered pages: a render function, its queries and its handlers")

// EditorKinds is the ui.editorKind point: pages that open as their own
// top-level workbench tab, keyed by an id.
var EditorKinds = extensions.NewPoint[EditorKind]("ui.editorKind", "Application pages that open as their own top-level workbench tab")

// App is one application. Its Descriptor's ID is the app id, e.g. "hello".
type App struct {
	extensions.Descriptor
	Title       string
	Description string
	Icon        string // a Material icon name, e.g. "waving_hand"
}

// EditorKind is one kind of top-level tab. Its Descriptor's ID is
// "app:<appId>:<kind>"; "main" is the app's own tab.
type EditorKind struct {
	extensions.Descriptor
	App   string
	Kind  string
	Title string
	Icon  string
	Page  string // the page it shows, "<appId>/<page>"
}

// KindID is an editor kind's id.
func KindID(app, kind string) string { return "app:" + app + ":" + kind }

// QueryRef is a query a page holds while open, named for its bindings.
// Bind binds params to view state: param name → "state.<path>". When the
// state changes, the session subscribes again with the new params (a
// state value of nil leaves the param as Params has it).
type QueryRef struct {
	Query  string
	Params any
	Bind   map[string]string
}

// Subject is what a page's tab reports to the workbench context.
type Subject struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Page is one page. Its Descriptor's ID is "<appId>/<page>".
type Page struct {
	extensions.Descriptor
	App         string
	Title       string
	Description string
	// Action is the authz action opening it needs on the tenant.
	Action string
	// Queries are the queries the page binds to, by name ("query:<name>").
	Queries func(params json.RawMessage) map[string]QueryRef
	// Subject is what its tab is showing, from the params.
	Subject func(params json.RawMessage) Subject
	// Setup runs each time a session opens, before the page's queries
	// open: for what the page needs to exist, such as an app's entity
	// types. A failure fails the opening.
	Setup func(h *Handle) error
	// OnOpen sets up the view state of a new session.
	OnOpen func(h *Handle) error
	// Render returns the page's tree. It reads view state and query data
	// through r, and wraps independent parts in r.Region, so a change
	// re-renders only the regions that read it.
	Render   func(r *Render) *Node
	Handlers map[string]func(h *Handle) error
}

// Commander decides commands for handlers (the live bus).
type Commander interface {
	Execute(args features.ServiceArgs, id, typ, target, request string, raw json.RawMessage) ([]live.Problem, *features.ServiceResponse)
}

// Action is a client action: an effect only the browser can perform.
type Action struct {
	Type     string `json:"type"` // toast, navigate, openEditor, focus, download, copy, confirm, rename
	Message  string `json:"message,omitempty"`
	Severity string `json:"severity,omitempty"` // toast: success, info, warning, error
	// openEditor
	Kind     string          `json:"kind,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	HelpText string          `json:"helpText,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
	// navigate, focus (a key path), download, copy
	Page     string   `json:"page,omitempty"`
	Path     []string `json:"path,omitempty"`
	URL      string   `json:"url,omitempty"`
	Text     string   `json:"text,omitempty"`
	FileName string   `json:"fileName,omitempty"`
	// confirm: the answer is posted to Handler with {confirmed}
	Handler string `json:"handler,omitempty"`
}

// Handle is what a handler gets: the event's payload, the session's view
// state, commands and client actions.
type Handle struct {
	features.ServiceArgs
	Payload map[string]any
	Params  json.RawMessage
	s       *Session
	request string
	actions []Action
}

// State reads view state at a dotted path.
func (h *Handle) State(path string) any { return getPath(h.s.state, path) }

// Set changes view state at a dotted path; regions and props that read it
// are patched after the handler.
func (h *Handle) Set(path string, v any) {
	setPath(h.s.state, path, v)
	h.s.changed = append(h.s.changed, "state."+path)
}

// Value reads the event's payload at a dotted path.
func (h *Handle) Value(path string) any { return getPath(h.Payload, path) }

// Page is the page the handler runs on.
func (h *Handle) Page() Page { return h.s.page }

// String reads a payload value as a string.
func (h *Handle) String(name string) string {
	s, _ := h.Payload[name].(string)
	return s
}

// Command sends a command as the session's user; its effect comes back
// through the page's queries like anyone else's.
func (h *Handle) Command(typ, target string, args any) ([]live.Problem, *features.ServiceResponse) {
	raw, _ := json.Marshal(args)
	problems, fail := h.s.o.Commander.Execute(h.ServiceArgs, "ui-"+newID(), typ, target, h.request, raw)
	return problems, fail
}

// Query reads a query's result once, now, as the handle's principal: for
// a decision, never for what the page shows, which it binds.
func (h *Handle) Query(query string, params any) (any, *features.ServiceResponse) {
	if h.s.o.Subs == nil {
		return nil, h.Fail(http.StatusServiceUnavailable, Log.Code("no_queries").New("there are no queries"))
	}
	raw, _ := json.Marshal(params)
	data, _, fail := h.s.o.Subs.Once(h.ServiceArgs, query, raw)
	return data, fail
}

// Do adds a client action.
func (h *Handle) Do(a Action) { h.actions = append(h.actions, a) }

// Toast shows a message.
func (h *Handle) Toast(severity, message string) {
	h.Do(Action{Type: "toast", Severity: severity, Message: message})
}

// OpenEditor opens a page of the app as its own top-level tab, of the
// editor kind app:<appId>:<kind>.
func (h *Handle) OpenEditor(kind, id, name, helpText string, params any) {
	raw, _ := json.Marshal(params)
	h.Do(Action{Type: "openEditor", Kind: KindID(h.s.page.App, kind), ID: id, Name: name, HelpText: helpText, Params: raw})
}

// Render is what a render function reads through, so the session knows
// which region reads what.
type Render struct {
	s    *Session
	deps map[string]bool
}

// Params are the page's parameters.
func (r *Render) Params() json.RawMessage { return r.s.params }

// Principal is the viewer.
func (r *Render) Principal() features.Principal { return r.s.principal }

// State reads view state at a dotted path.
func (r *Render) State(path string) any {
	r.deps["state."+path] = true
	return getPath(r.s.state, path)
}

// Query reads a held query's data, at an optional dotted path.
func (r *Render) Query(name string, path ...string) any {
	r.deps["query:"+name] = true
	q := r.s.queries[name]
	if q == nil {
		return nil
	}
	if len(path) == 0 {
		return q.data
	}
	return getPath(q.data, path[0])
}

// Region renders an independent part of the page: when what fn read
// changes, only it is rendered again. Its root's key must be key.
func (r *Render) Region(key string, fn func(r *Render) *Node) *Node {
	inner := &Render{s: r.s, deps: map[string]bool{}}
	n := fn(inner)
	if n == nil {
		n = N("mui.box", key)
	}
	n.Key = key
	r.s.pending[n] = &region{fn: fn, deps: inner.deps}
	return n
}
