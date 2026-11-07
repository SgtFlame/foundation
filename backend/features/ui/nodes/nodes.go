// Package uinodes is the server-driven UI's Cognition node family and graph
// kinds (forge 02-Blueprints, documentation/Blueprints.md): ui.handler
// graphs are a page's handlers, run in the UI session at each event, and
// prefab.logic graphs a prefab instance's logic, run on mount, props and
// query changes and unmount (forge 04b). Runner runs both for sessions.
package uinodes

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Kinds and the On event node type.
const (
	KindHandler = "ui.handler"
	KindPrefab  = "prefab.logic"
	OnEvent     = "ui.onEvent"
)

// Host is what UI nodes act on: a session's handle, or a test run's host.
type Host interface {
	State(path string) any
	Set(path string, v any)
	Value(path string) any // the event's payload
	Do(a ui.Action)
	OpenEditor(kind, id, name, helpText string, params any)
	Command(typ, target string, args any) ([]live.Problem, *features.ServiceResponse)
}

// PrefabHost is a host that can run for a prefab instance (forge 04b): a
// session's handle, or a test run's host. Outside an instance, Prop and
// Held are nil and Raise and Subscribe fail.
type PrefabHost interface {
	Host
	Prop(path string) any
	Held(query string) any
	Raise(event string, payload map[string]any) error
	Subscribe(path, query string, params any) error
}

// Deps are what the runner needs, made after the registry.
type Deps struct {
	Engine   *engine.Engine
	Presence *presence.Presence
}

// Limits for a handler: a click must be quick.
var handlerLimits = &engine.Limits{MaxSteps: 500, MaxWallMs: 3000, MaxLoop: 500}

// Runner runs ui.handler graphs for UI sessions (ui.HandlerRunner).
type Runner struct{ Deps func() Deps }

// graphFor is the graph a handle runs: its published version, or in a dry
// page (the Prefab Editor's Canvas, forge 04c) its draft.
// An installed app's page runs the copy its app version made (forge 05d).
func graphFor(d Deps, h *ui.Handle, id string) (cognition.Graph, bool) {
	if h.Dry() {
		return d.Engine.Draft(h.ServiceArgs, id)
	}
	if pinned := h.Page().Graph; pinned != nil {
		if g, ok := pinned(id); ok {
			if g, ok := g.(cognition.Graph); ok {
				return g, true
			}
		}
	}
	return d.Engine.Graph(h.ServiceArgs, id)
}

// RunHandler runs the published ui.handler graph id with the event's
// payload; a republished handler runs from the next event on (Blueprints
// question 3). It is traced in full while open in an editor, else only
// when it fails (question 4).
func (r Runner) RunHandler(h *ui.Handle, id string) (bool, error) {
	d := r.Deps()
	g, ok := graphFor(d, h, id)
	if !ok || g.Kind != KindHandler {
		return false, nil
	}
	trace := engine.TraceFailures
	if d.Presence != nil && d.Presence.Here(h.Context(), h.Principal.TenantID, "graph."+id) {
		trace = engine.TraceAll
	}
	in := cognition.Values{"payload": h.Payload}
	for k, v := range h.Payload {
		in[k] = v
	}
	res, err := d.Engine.Run(engine.RunArgs{ServiceArgs: h.ServiceArgs, Graph: &g, Event: OnEvent, Inputs: in, Trigger: "ui:" + h.Page().ID,
		Host: h, Trace: trace, Limits: handlerLimits})
	if err != nil {
		// The graph is the user's own work: say where it failed and why
		at := res.FailedNode
		if n, ok := g.NodeByID(at); ok && n.Title != "" {
			at = n.Title
		}
		return true, log.Code("handler_failed").With("graphId", id, "nodeId", res.FailedNode, "runId", res.RunID).
			Public(fmt.Sprintf("%s failed at %s: %s. Its run is in the graph's Log Viewer.", g.Name, at, err.Error())).Wrap(err)
	}
	return true, nil
}

// RunLogic runs the published prefab.logic graph id for a lifecycle event
// of the instance h runs for, with its props; found is false when the
// graph is not one or has no node for the event (ui.LogicRunner).
func (r Runner) RunLogic(h *ui.Handle, id, event string) (bool, error) {
	d := r.Deps()
	g, ok := graphFor(d, h, id)
	if !ok || g.Kind != KindPrefab || !slices.ContainsFunc(g.Nodes, func(n cognition.Node) bool { return n.Type == event }) {
		return false, nil
	}
	trace := engine.TraceFailures
	if d.Presence != nil && d.Presence.Here(h.Context(), h.Principal.TenantID, "graph."+id) {
		trace = engine.TraceAll
	}
	res, err := d.Engine.Run(engine.RunArgs{ServiceArgs: h.ServiceArgs, Graph: &g, Event: event, Inputs: cognition.Values{"props": h.Prop("")},
		Trigger: "ui:" + h.Page().ID, Host: h, Trace: trace, Limits: handlerLimits})
	if err != nil {
		at := res.FailedNode
		if n, ok := g.NodeByID(at); ok && n.Title != "" {
			at = n.Title
		}
		return true, log.Code("logic_failed").With("graphId", id, "nodeId", res.FailedNode, "runId", res.RunID).
			Public(fmt.Sprintf("%s failed at %s: %s. Its run is in the graph's Log Viewer.", g.Name, at, err.Error())).Wrap(err)
	}
	return true, nil
}

var log = ui.Log.Tags("blueprint")

// TestHost is a handler test run's host: its view state starts from the
// inputs' state, and it reports what changed, the client actions and the
// commands it would have sent, without sending them.
type TestHost struct {
	state   map[string]any
	payload map[string]any
	props   map[string]any
	Changes map[string]any   `json:"stateChanges"`
	Actions []ui.Action      `json:"actions"`
	Sent    []map[string]any `json:"commands"`
	Raised  []map[string]any `json:"raised"`
	Subs    []map[string]any `json:"subscriptions"`
}

func (t *TestHost) State(path string) any { return getPath(t.state, path) }
func (t *TestHost) Set(path string, v any) {
	t.state = setPath(t.state, path, v).(map[string]any)
	t.Changes[path] = v
}
func (t *TestHost) Value(path string) any { return getPath(t.payload, path) }
func (t *TestHost) Do(a ui.Action)        { t.Actions = append(t.Actions, a) }
func (t *TestHost) OpenEditor(kind, id, name, helpText string, params any) {
	raw, _ := json.Marshal(params)
	t.Do(ui.Action{Type: "openEditor", Kind: kind, ID: id, Name: name, HelpText: helpText, Params: raw})
}
func (t *TestHost) Command(typ, target string, args any) ([]live.Problem, *features.ServiceResponse) {
	t.Sent = append(t.Sent, map[string]any{"command": typ, "target": target, "args": args})
	return nil, nil
}
func (t *TestHost) Prop(path string) any  { return getPath(t.props, path) }
func (t *TestHost) Held(query string) any { return nil }
func (t *TestHost) Raise(event string, payload map[string]any) error {
	t.Raised = append(t.Raised, map[string]any{"event": event, "payload": payload})
	return nil
}
func (t *TestHost) Subscribe(path, query string, params any) error {
	t.Subs = append(t.Subs, map[string]any{"path": path, "query": query, "params": params})
	return nil
}

func getPath(v any, path string) any {
	m, _ := v.(map[string]any)
	if path == "" {
		return v
	}
	head, rest, more := cut(path)
	if !more {
		return m[head]
	}
	return getPath(m[head], rest)
}

func setPath(v any, path string, value any) any {
	m, _ := v.(map[string]any)
	out := map[string]any{}
	for k, x := range m {
		out[k] = x
	}
	head, rest, more := cut(path)
	if !more {
		out[head] = value
	} else {
		out[head] = setPath(out[head], rest, value)
	}
	return out
}

func cut(path string) (string, string, bool) {
	for i := range path {
		if path[i] == '.' {
			return path[:i], path[i+1:], true
		}
	}
	return path, "", false
}

func host(x *cognition.Exec) (Host, error) {
	h, ok := x.Host.(Host)
	if !ok {
		return nil, fmt.Errorf("%s runs only in a UI handler", x.Node.ID)
	}
	return h, nil
}

// A session's handle runs prefab handlers and logic.
var _ PrefabHost = (*ui.Handle)(nil)

func prefabHost(x *cognition.Exec) (PrefabHost, error) {
	h, ok := x.Host.(PrefabHost)
	if !ok {
		return nil, fmt.Errorf("%s runs only in a prefab's handler or logic", x.Node.ID)
	}
	return h, nil
}

func node(reg *extensions.Registry, order int, t cognition.NodeType) {
	t.Order, t.Source = order, "ui"
	if t.Category == "" {
		t.Category = "UI"
	}
	if t.Kinds == nil {
		t.Kinds = []string{KindHandler, KindPrefab}
	}
	cognition.Nodes.Register(reg, t)
}

func then(in ...cognition.Pin) cognition.Pins {
	return cognition.Pins{In: append([]cognition.Pin{cognition.ExecPin(cognition.Then, "")}, in...), Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")}}
}

func str(in cognition.Values, pin, fallback string) string {
	if s, ok := in[pin].(string); ok && s != "" {
		return s
	}
	if v, ok := in[pin]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return fallback
}

// Contribute registers the kinds and nodes.
func Contribute(reg *extensions.Registry) {
	testHost := func(_ features.ServiceArgs, _ cognition.Graph, inputs cognition.Values) (any, func() any) {
		state, _ := inputs["state"].(map[string]any)
		props, _ := inputs["props"].(map[string]any)
		payload, _ := inputs["payload"].(map[string]any)
		if payload == nil {
			payload = map[string]any{}
			for k, v := range inputs {
				if k != "state" && k != "props" {
					payload[k] = v
				}
			}
			inputs["payload"] = payload
		}
		if state == nil {
			state = map[string]any{}
		}
		h := &TestHost{state: state, payload: payload, props: props, Changes: map[string]any{}, Actions: []ui.Action{}, Sent: []map[string]any{},
			Raised: []map[string]any{}, Subs: []map[string]any{}}
		return h, func() any { return h }
	}
	cognition.Kinds.Register(reg, cognition.Kind{
		Descriptor: extensions.Descriptor{ID: KindHandler, Order: 200, Source: "ui"}, Title: "UI handler", Events: []string{OnEvent},
		Description: "A page's or a prefab's handler: runs in the user's UI session when a component's event names it, at once, and changes view state, shows client actions, sends commands and, in a prefab, raises its events.",
		Returns:     "view-state changes, client actions, raised events and the commands it sent",
		Host:        testHost,
	})
	cognition.Kinds.Register(reg, cognition.Kind{
		Descriptor: extensions.Descriptor{ID: KindPrefab, Order: 210, Source: "ui"}, Title: "Prefab logic",
		Events:      []string{"ui.onMount", "ui.onPropsChanged", "ui.onQueryChanged", "ui.onUnmount"},
		Description: "A prefab instance's logic: runs after the instance is first drawn, when its props or one of its queries change, and when it goes. It can do what a handler can, raise the prefab's events and keep subscriptions (Subscribe to records), which end with the instance. One instance's runs are one at a time; an unmount run shows nothing.",
		Returns:     "view-state changes, client actions, raised events, subscriptions and commands",
		Host:        testHost,
	})

	node(reg, 100, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: OnEvent}, Title: "On event", Category: "Events", Event: true, Kinds: []string{KindHandler},
		Description: "Starts a handler when a component's event names it: the event's payload, with the fields declared here as their own pins",
		Example:     "On event with fields source and target, for a graph canvas's connect",
		Config: json.RawMessage(`{"type":"object","properties":{"outputs":{"type":"array","title":"Payload fields","items":{"type":"object","properties":{
  "id":{"type":"string","title":"ID"},"name":{"type":"string","title":"Name"},"type":{"type":"string","title":"Type"},"description":{"type":"string","title":"Description"}}}}}}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			fields, err := cognition.DeclaredPins(config)
			if err != nil {
				return cognition.Pins{}, err
			}
			return cognition.Pins{Out: append([]cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("payload", "Payload", "json", "The whole payload")}, fields...)}, nil
		},
	})
	for i, e := range []struct{ id, title string }{{"ui.onMount", "On mount"}, {"ui.onPropsChanged", "On props changed"}, {"ui.onQueryChanged", "On query changed"}, {"ui.onUnmount", "On unmount"}} {
		node(reg, 110+i, cognition.NodeType{
			Descriptor: extensions.Descriptor{ID: e.id}, Title: e.title, Category: "Events", Event: true, Kinds: []string{KindPrefab},
			Description: "Starts a prefab instance's logic " + e.title[3:] + ", with the instance's props", Example: e.title + " to load the prefab's data",
			Pins: cognition.Pins{Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("props", "Props", "json", "The instance's props")}},
		})
	}

	pathSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","title":"Path","description":"A dotted path into view state, e.g. greeting"}},"required":["path"]}`)
	node(reg, 200, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.getState"}, Title: "Get state", Volatile: true,
		Description: "Reads the session's view state at a path",
		Example:     "Get state tab, to know which sub-tab is showing",
		Config:      pathSchema,
		Pins:        cognition.Pins{Out: []cognition.Pin{cognition.DataPin("value", "Value", cognition.Any, "The state's value")}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			h, err := host(x)
			if err != nil {
				return nil, "", err
			}
			var c struct{ Path string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			return cognition.Values{"value": h.State(c.Path)}, "", nil
		},
	})
	node(reg, 210, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.setState"}, Title: "Set state",
		Description: "Changes the session's view state at a path; what reads it is drawn again for this user",
		Example:     "Set state greeting to \"Hello, \" + name",
		Config:      pathSchema,
		Pins:        then(cognition.Pin{ID: "value", Name: "Value", Type: cognition.Any, Optional: true, Description: "The new value"}),
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			h, err := host(x)
			if err != nil {
				return nil, "", err
			}
			var c struct{ Path string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if c.Path == "" {
				return nil, "", fmt.Errorf("Set state has no path")
			}
			h.Set(c.Path, in["value"])
			return nil, cognition.Then, nil
		},
	})
	node(reg, 220, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.getLocal"}, Title: "Get local", Volatile: true,
		Description: "Reads the event's payload at a path, such as a field the browser filled in from its local state",
		Example:     "Get local name, the text the user typed",
		Config:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","title":"Path"}}}`),
		Pins:        cognition.Pins{Out: []cognition.Pin{cognition.DataPin("value", "Value", cognition.Any, "The payload's value")}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			h, err := host(x)
			if err != nil {
				return nil, "", err
			}
			var c struct{ Path string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			return cognition.Values{"value": h.Value(c.Path)}, "", nil
		},
	})

	node(reg, 230, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.getProp"}, Title: "Get prop", Volatile: true,
		Description: "Reads the prefab instance's props at a path; the whole props when the path is empty",
		Example:     "Get prop task._id, the id of the row's task",
		Config:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","title":"Path","description":"A dotted path into the props, e.g. task._id"}}}`),
		Pins:        cognition.Pins{Out: []cognition.Pin{cognition.DataPin("value", "Value", cognition.Any, "The prop's value")}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			h, err := prefabHost(x)
			if err != nil {
				return nil, "", err
			}
			var c struct{ Path string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			return cognition.Values{"value": h.Prop(c.Path)}, "", nil
		},
	})
	node(reg, 240, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.getQuery"}, Title: "Get query data", Volatile: true,
		Description: "Reads the data of a query the prefab declares, as the instance holds it now",
		Example:     "Get query data openTasks, to count them",
		Config:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","title":"Query","description":"The name the prefab declares it under"}},"required":["query"]}`),
		Pins:        cognition.Pins{Out: []cognition.Pin{cognition.DataPin("value", "Value", cognition.Any, "The query's data")}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			h, err := prefabHost(x)
			if err != nil {
				return nil, "", err
			}
			var c struct{ Query string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			return cognition.Values{"value": h.Held(c.Query)}, "", nil
		},
	})
	node(reg, 250, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.raiseEvent"}, Title: "Raise event", Effects: true,
		Description: "Raises one of the prefab's declared events with a payload: the handler the instance's user maps it to runs at once, in the user's scope (as a React component calls its onX prop). Not the command graphs' Emit event.",
		Example:     "Raise event toggled with the task's id and its new done",
		Config: json.RawMessage(`{"type":"object","properties":{"event":{"type":"string","title":"Event","description":"One of the prefab's declared events"},"inputs":{"type":"array","title":"Payload fields","items":{"type":"object","properties":{
  "id":{"type":"string","title":"ID"},"name":{"type":"string","title":"Name"},"type":{"type":"string","title":"Type"},"description":{"type":"string","title":"Description"}}}}},"required":["event"]}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c struct {
				Inputs []cognition.Pin `json:"inputs"`
			}
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			pins := then(cognition.Pin{ID: "payload", Name: "Payload", Type: "json", Optional: true, Description: "The whole payload; the fields' pins add to it"})
			for i, p := range c.Inputs {
				if p.ID == "" || p.Type == "" {
					return cognition.Pins{}, fmt.Errorf("payload field %d needs an id and a type", i+1)
				}
				if p.Name == "" {
					p.Name = p.ID
				}
				p.Optional = true
				pins.In = append(pins.In, p)
			}
			return pins, nil
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			h, err := prefabHost(x)
			if err != nil {
				return nil, "", err
			}
			var c struct {
				Event  string
				Inputs []cognition.Pin
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			payload := map[string]any{}
			if p, ok := in["payload"].(map[string]any); ok {
				for k, v := range p {
					payload[k] = v
				}
			}
			for _, p := range c.Inputs {
				if v, ok := in[p.ID]; ok && v != nil {
					payload[p.ID] = v
				}
			}
			return nil, cognition.Then, h.Raise(c.Event, payload)
		},
	})
	node(reg, 260, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.subscribe"}, Title: "Subscribe to records", Effects: true, Kinds: []string{KindPrefab},
		Description: "Keeps a query's data (records.find by default) at a path of the instance's state while it is mounted: the state follows every change, and the subscription ends with the instance. Subscribing the same path again with other params replaces it.",
		Example:     "Subscribe to records into tasks, with {entity: task, where: {done: false}}",
		Config:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","title":"State path","description":"Where the data is kept, e.g. tasks"},"query":{"type":"string","title":"Query","description":"records.find when empty"}},"required":["path"]}`),
		Pins:        then(cognition.DataPin("params", "Params", "json", "The query's params, e.g. {entity, where, sort}")),
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			h, err := prefabHost(x)
			if err != nil {
				return nil, "", err
			}
			var c struct{ Path, Query string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if c.Query == "" {
				c.Query = "records.find"
			}
			return nil, cognition.Then, h.Subscribe(c.Path, c.Query, in["params"])
		},
	})

	effect := func(order int, id, title, description, example, config string, pins []cognition.Pin, fn func(h Host, x *cognition.Exec, in cognition.Values) error) {
		node(reg, order, cognition.NodeType{
			Descriptor: extensions.Descriptor{ID: id}, Title: title, Effects: true, Description: description, Example: example,
			Config: json.RawMessage(config), Pins: then(pins...),
			Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
				h, err := host(x)
				if err != nil {
					return nil, "", err
				}
				return nil, cognition.Then, fn(h, x, in)
			},
		})
	}
	effect(300, "ui.toast", "Toast", "Shows the user a short message", "Toast success \"Counter reset\"",
		`{"type":"object","properties":{"severity":{"type":"enum","title":"Severity","enum":["success","info","warning","error"]},"message":{"type":"string","title":"Message","description":"Used when the input is unconnected"}}}`,
		[]cognition.Pin{{ID: "message", Name: "Message", Type: "string", Optional: true, Description: "The message; the config's when unconnected"}},
		func(h Host, x *cognition.Exec, in cognition.Values) error {
			var c struct{ Severity, Message string }
			if err := x.Config(&c); err != nil {
				return err
			}
			if c.Severity == "" {
				c.Severity = "info"
			}
			h.Do(ui.Action{Type: "toast", Severity: c.Severity, Message: str(in, "message", c.Message)})
			return nil
		})
	effect(310, "ui.openEditor", "Open editor", "Opens one of the app's pages as its own top-level tab, of an editor kind", "Open editor item with the list item's id",
		`{"type":"object","properties":{"kind":{"type":"string","title":"Editor kind","description":"The app's kind, e.g. item"},"helpText":{"type":"string","title":"Help text"}},"required":["kind"]}`,
		[]cognition.Pin{cognition.DataPin("id", "ID", "string", "The object's id"), {ID: "name", Name: "Name", Type: "string", Optional: true, Description: "The tab's name"},
			{ID: "params", Name: "Params", Type: cognition.Any, Optional: true, Description: "The page's params; {id} when unconnected"}},
		func(h Host, x *cognition.Exec, in cognition.Values) error {
			var c struct{ Kind, HelpText string }
			if err := x.Config(&c); err != nil {
				return err
			}
			id := str(in, "id", "")
			params := in["params"]
			if params == nil {
				params = map[string]string{"id": id}
			}
			h.OpenEditor(c.Kind, id, str(in, "name", id), c.HelpText, params)
			return nil
		})
	effect(315, "ui.navigate", "Navigate", "Goes to a route of the app: in its own tab, the page at that route replaces the current one, and Back returns to it; elsewhere the app's tab opens there", "Navigate to /task/ with the row's id appended",
		`{"type":"object","properties":{"route":{"type":"string","title":"Route","description":"A path of the app, e.g. /mine; used when the input is unconnected"}}}`,
		[]cognition.Pin{{ID: "route", Name: "Route", Type: "string", Optional: true, Description: "The path, with its params filled (/task/123); the config's when unconnected"}},
		func(h Host, x *cognition.Exec, in cognition.Values) error {
			var c struct{ Route string }
			if err := x.Config(&c); err != nil {
				return err
			}
			route := str(in, "route", c.Route)
			if route == "" || route[0] != '/' {
				return fmt.Errorf("%q is not a route: a route starts with /", route)
			}
			h.Do(ui.Action{Type: "navigate", Page: route})
			return nil
		})
	effect(320, "ui.confirm", "Confirm", "Asks the user to confirm; their answer runs another handler graph with {confirmed}", "Confirm \"Reset the counter?\" then hello-onResetConfirmed",
		`{"type":"object","properties":{"message":{"type":"string","title":"Message"},"handler":{"type":"string","title":"Handler","description":"The handler graph the answer goes to"}},"required":["handler"]}`,
		[]cognition.Pin{{ID: "message", Name: "Message", Type: "string", Optional: true, Description: "The question; the config's when unconnected"}},
		func(h Host, x *cognition.Exec, in cognition.Values) error {
			var c struct{ Message, Handler string }
			if err := x.Config(&c); err != nil {
				return err
			}
			h.Do(ui.Action{Type: "confirm", Message: str(in, "message", c.Message), Handler: c.Handler})
			return nil
		})
	effect(330, "ui.download", "Download", "Gives the user a file to save", "Download the export as items.json",
		`{"type":"object","properties":{"fileName":{"type":"string","title":"File name"}}}`,
		[]cognition.Pin{cognition.DataPin("text", "Text", "string", "The file's content")},
		func(h Host, x *cognition.Exec, in cognition.Values) error {
			var c struct{ FileName string }
			if err := x.Config(&c); err != nil {
				return err
			}
			h.Do(ui.Action{Type: "download", Text: str(in, "text", ""), FileName: c.FileName})
			return nil
		})
	effect(340, "ui.copy", "Copy", "Copies text to the user's clipboard", "Copy the share link",
		`{"type":"object","properties":{}}`,
		[]cognition.Pin{cognition.DataPin("text", "Text", "string", "The text to copy")},
		func(h Host, _ *cognition.Exec, in cognition.Values) error {
			h.Do(ui.Action{Type: "copy", Text: str(in, "text", "")})
			return nil
		})
}
