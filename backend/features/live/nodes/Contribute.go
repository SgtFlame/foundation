package livenodes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

const source = "live"

func node(reg *extensions.Registry, order int, t cognition.NodeType) {
	t.Order, t.Source = order, source
	if t.Category == "" {
		t.Category = "Commands"
	}
	cognition.Nodes.Register(reg, t)
}

func declared(config json.RawMessage, first ...cognition.Pin) (cognition.Pins, error) {
	fields, err := cognition.DeclaredPins(config)
	if err != nil {
		return cognition.Pins{}, err
	}
	return cognition.Pins{Out: append(first, fields...)}, nil
}

const fieldsSchema = `{"type":"object","properties":{"outputs":{"type":"array","title":"Fields","description":"Fields read from it as their own pins","items":{"type":"object","properties":{
  "id":{"type":"string","title":"ID"},"name":{"type":"string","title":"Name"},"type":{"type":"string","title":"Type"},"description":{"type":"string","title":"Description"}}}}}}`

// Contribute registers the kinds and nodes; deps returns their
// dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	idFor := func(field string, id func(string) string) func(json.RawMessage, string) ([]string, error) {
		return func(spec json.RawMessage, owner string) ([]string, error) {
			var s map[string]any
			_ = json.Unmarshal(spec, &s)
			name, _ := s[field].(string)
			if name == "" {
				return nil, fmt.Errorf("the spec names no %s", field)
			}
			if app, ok := strings.CutPrefix(owner, "app:"); ok {
				return []string{id(app + "." + name), id(name)}, nil
			}
			return []string{id(name)}, nil
		}
	}
	for i, k := range []cognition.Kind{
		{Descriptor: extensions.Descriptor{ID: KindCommand}, Title: "Command", Events: []string{OnCommand}, Pure: true,
			Description: "Decides a command: from its args and its target's state, the events to commit (Emit event), or problems (Reject). It may read but has no other side effects, and is registered as the command its spec names when published.",
			Returns:     "the events to commit, or problems", Spec: json.RawMessage(commandSpecSchema), IDFor: idFor("command", CommandGraphID),
			Host: func(features.ServiceArgs, cognition.Graph, cognition.Values) (any, func() any) {
				h := &CommandHost{}
				return h, func() any { return h }
			}},
		{Descriptor: extensions.Descriptor{ID: KindAction}, Title: "Server action", Events: []string{cognition.EventCall},
			Description: "Server logic with typed inputs and outputs, called from handlers and other graphs with Call graph; it changes data only by sending commands, and may call outside services.",
			Returns:     "its declared outputs",
			Host: func(features.ServiceArgs, cognition.Graph, cognition.Values) (any, func() any) {
				h := &TestSender{}
				return h, func() any { return h }
			}},
		{Descriptor: extensions.Descriptor{ID: KindQuery}, Title: "Query", Events: []string{OnQuery}, Pure: true,
			Description: "A live query: from its params, a result read from projections, with no side effects. It is registered as the query its spec names when published, and every subscription to it runs it again when what it read changes.",
			Returns:     "the result (Return's output result)", Spec: json.RawMessage(querySpecSchema), IDFor: idFor("query", QueryGraphID),
			Host: func(features.ServiceArgs, cognition.Graph, cognition.Values) (any, func() any) {
				h := &QueryHost{Pos: live.Position{}}
				return h, func() any { return h }
			}},
		{Descriptor: extensions.Descriptor{ID: KindTrigger}, Title: "Trigger", Events: []string{"live.onEvent", "live.onSchedule", "live.onWebhook"},
			Description: "Runs in the background when an event is committed, on a schedule, or on a webhook (runs come with forge 07-Email).", Returns: "nothing"},
	} {
		k.Order, k.Source = 100+i*10, source
		cognition.Kinds.Register(reg, k)
	}

	node(reg, 500, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: OnCommand}, Title: "On command", Category: "Events", Event: true, Kinds: []string{KindCommand},
		Description: "Starts a command graph when its command is sent: its args, its target's id and the target's projected state",
		Example:     "On command hello.increment, reading args.by and state.value",
		Config:      json.RawMessage(fieldsSchema),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			return declared(config, cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("args", "Args", "json", "The command's args"),
				cognition.DataPin("state", "State", "json", "The target's projected state; nothing before its first event"),
				cognition.DataPin("target", "Target", "string", "The target's id"))
		},
	})
	node(reg, 510, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "live.emit"}, Title: "Emit event", Kinds: []string{KindCommand},
		Description: "Decides one event for the command to commit; the target's projector folds it into the new state",
		Example:     "Emit event incremented with data {by: 1}",
		Config:      json.RawMessage(`{"type":"object","properties":{"event":{"type":"string","title":"Event","description":"The event's type, one of the spec's events"}},"required":["event"]}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), {ID: "data", Name: "Data", Type: cognition.Any, Optional: true, Description: "The event's data"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Event string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			h, ok := x.Host.(*CommandHost)
			if !ok {
				return nil, "", fmt.Errorf("Emit event runs only in a command graph")
			}
			if !live.ValidToken(c.Event) {
				return nil, "", fmt.Errorf("%q is not an event type", c.Event)
			}
			h.Events = append(h.Events, live.NewEvent{Type: c.Event, Data: in["data"]})
			return nil, cognition.Then, nil
		},
	})
	node(reg, 520, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "live.reject"}, Title: "Reject", Kinds: []string{KindCommand},
		Description: "Refuses the command with a problem, in words a model can act on, and ends the run; nothing is committed",
		Example:     "Reject \"a node cannot be joined to itself\" when source == target",
		Config:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","title":"Path","description":"What is wrong, e.g. args.target"},"fix":{"type":"string","title":"Fix","description":"How to fix it"}}}`),
		Pins:        cognition.Pins{In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("message", "Message", "string", "Why the command is refused")}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Path, Fix string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			h, ok := x.Host.(*CommandHost)
			if !ok {
				return nil, "", fmt.Errorf("Reject runs only in a command graph")
			}
			msg, _ := cognition.Get[string](in, "message")
			h.Problems = append(h.Problems, live.Problem{Path: c.Path, Message: msg, Fix: c.Fix})
			return nil, "", nil
		},
	})
	node(reg, 530, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "live.sendCommand"}, Title: "Send command", Effects: true,
		Description: "Sends a command to change data, as the user (in a handler) or the run's principal, and continues by OK or Rejected. Everyone's queries over the target change once its events are projected.",
		Example:     "Send command hello.increment to target main with args {by: 1}, and Toast its message when Rejected",
		Config: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","title":"Command","description":"The command type, e.g. hello.increment (live.listCommands)"},
			"target":{"type":"string","title":"Target","description":"The object's id when the Target input is unconnected"}},"required":["command"]}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), {ID: "target", Name: "Target", Type: "string", Optional: true, Description: "The id of the object to change; the config's when unconnected"},
				{ID: "args", Name: "Args", Type: cognition.Any, Optional: true, Description: "The command's args, an object"}},
			Out: []cognition.Pin{cognition.ExecPin("ok", "OK"), cognition.ExecPin("rejected", "Rejected"),
				cognition.DataPin("problems", "Problems", "json", "Why it was refused: [{path, message, fix}]"),
				cognition.DataPin("message", "Message", "string", "The refusal's first message")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Command, Target string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			target, _ := cognition.Get[string](in, "target")
			if target == "" {
				target = c.Target
			}
			sender, ok := x.Host.(Sender)
			if !ok {
				d := deps()
				if d.Bus == nil {
					return nil, "", fmt.Errorf("there is no command bus")
				}
				sender = busSender{args: x.ServiceArgs, bus: d.Bus}
			}
			args := in["args"]
			if args == nil {
				args = map[string]any{}
			}
			problems, fail := sender.Command(c.Command, target, args)
			if fail != nil {
				msg := fail.Message
				if len(problems) > 0 {
					msg = problems[0].Message
				}
				if problems == nil {
					problems = []live.Problem{{Message: msg}}
				}
				return cognition.Values{"problems": problems, "message": msg}, "rejected", nil
			}
			return cognition.Values{"problems": []live.Problem{}, "message": ""}, "ok", nil
		},
	})
	node(reg, 540, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "live.readProjection"}, Title: "Read projection", Category: "Data",
		Description: "Reads one value of a projection by its key (\"<type>.<id>\"); a query graph runs again when it changes",
		Example:     "Read projection hello.counter at counter.main, giving {value}",
		Config:      json.RawMessage(`{"type":"object","properties":{"projector":{"type":"string","title":"Projector","description":"e.g. hello.counter"}},"required":["projector"]}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("key", "Key", "string", "The key, \"<type>.<id>\"")},
			Out: []cognition.Pin{cognition.DataPin("value", "Value", "json", "The value; nothing when absent"), cognition.DataPin("exists", "Exists", "bool", "Whether the key has a value")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Projector string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			key, _ := cognition.Get[string](in, "key")
			d := deps()
			if d.Store == nil {
				return nil, "", fmt.Errorf("there is no projection store")
			}
			if q, ok := x.Host.(*QueryHost); ok {
				q.Reads = append(q.Reads, Read{Projector: c.Projector, Key: key})
			}
			v, ok, err := d.Store.Get(x.Context(), x.Principal.TenantID, c.Projector, key)
			if err != nil {
				return nil, "", err
			}
			if q, isQ := x.Host.(*QueryHost); isQ && ok {
				q.Pos = q.Pos.Merge(v.Position())
			}
			var value any
			if ok && v.Value != nil {
				_ = json.Unmarshal(v.Value, &value)
			}
			return cognition.Values{"value": value, "exists": ok && v.Value != nil}, "", nil
		},
	})
	node(reg, 550, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "live.runQuery"}, Title: "Run query", Category: "Data", Volatile: true,
		Description: "Reads a query's result once, now (live.listQueries). For data a page displays, bind the query instead: bindings stay live.",
		Example:     "Run query hello.graph with {id: main} to check a node exists before connecting",
		Config:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","title":"Query"}},"required":["query"]}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{{ID: "params", Name: "Params", Type: cognition.Any, Optional: true, Description: "The query's params, an object"}},
			Out: []cognition.Pin{cognition.DataPin("data", "Data", "json", "The query's result")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Query string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			d := deps()
			if d.Subscriptions == nil {
				return nil, "", fmt.Errorf("there are no queries")
			}
			params := in["params"]
			if params == nil {
				params = map[string]any{}
			}
			raw, _ := json.Marshal(params)
			data, _, fail := d.Subscriptions.Once(x.ServiceArgs, c.Query, raw)
			if fail != nil {
				return nil, "", fmt.Errorf("%s", fail.Message)
			}
			return cognition.Values{"data": data}, "", nil
		},
	})
	node(reg, 555, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "live.currentUser"}, Title: "Current user", Category: "Data",
		Description: "Who the run is for: the user who sent the event, command or query (ids as text, as a relation to user holds them); no user for background work",
		Example:     "Current user's id as a new task's assignee",
		Pins: cognition.Pins{Out: []cognition.Pin{
			cognition.DataPin("user", "User", "string", "The user's id; empty for background work"),
			cognition.DataPin("tenant", "Tenant", "string", "The tenant's id")}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			user := ""
			if x.Principal.Kind == features.PrincipalUser {
				user = strconv.FormatUint(x.Principal.UserID, 10)
			}
			return cognition.Values{"user": user, "tenant": strconv.FormatUint(x.Principal.TenantID, 10)}, "", nil
		},
	})
	node(reg, 560, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: OnQuery}, Title: "On query", Category: "Events", Event: true, Kinds: []string{KindQuery},
		Description: "Starts a query graph when the query is read: its params. Return its result as the output result.",
		Example:     "On query with param id, feeding \"counter.\" + id to Read projection",
		Config:      json.RawMessage(fieldsSchema),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			return declared(config, cognition.ExecPin(cognition.Then, ""), cognition.DataPin("params", "Params", "json", "The query's params"))
		},
	})
	for i, e := range []struct{ id, title, description, example string }{
		{"live.onEvent", "On event", "Starts a trigger when an event is committed, such as a record changed", "On event deals.stageChanged to email the owner"},
		{"live.onSchedule", "On schedule", "Starts a trigger on a schedule", "On schedule every morning at 8 to send a digest"},
		{"live.onWebhook", "On webhook", "Starts a trigger when its webhook is called", "On webhook from the payment provider"},
	} {
		node(reg, 600+i, cognition.NodeType{
			Descriptor: extensions.Descriptor{ID: e.id}, Title: e.title, Category: "Events", Event: true, Kinds: []string{KindTrigger},
			Description: e.description + " (runs come with forge 07-Email)", Example: e.example,
			Pins: cognition.Pins{Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("event", "Event", "json", "What started it")}},
		})
	}
}
