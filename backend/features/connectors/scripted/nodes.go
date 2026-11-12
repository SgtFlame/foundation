// Package scripted is scripted connectors (forge 07d,
// documentation/Connectors.md): connector types an app declares in its
// manifest and implements in blueprints. It holds the connector.op graph
// kind and its nodes (Operation called, Return error), Emit connector
// event, and the scripted adapter: in every process, per tenant, an
// adapter.Serve over the tenant's installed scripted types, whose
// sessions answer each call by running the operation's pinned graph.
// A dispatcher cannot tell its replies from a builtin adapter's.
package scripted

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	connectorsnodes "github.com/SgtFlame/foundation/backend/features/connectors/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

var log = logging.In("connectors").Tags("scripted")

const source = "connectors"

// NodeEmit is Emit connector event.
const NodeEmit = "connector.emit"

// ErrorKey is the output a Return error node leaves the typed error in.
const ErrorKey = "$error"

// Host is what a connector.op graph's run acts on: for a watch graph, the
// events its Emit nodes give, published by the event source with the new
// cursor after the run.
type Host struct {
	Watch  bool
	Events []adapter.Event
}

// OperationConfig is Operation called's config, with the pins Normalize
// fills from the contract.
type OperationConfig struct {
	apps.OperationConfig
	Outputs []cognition.Pin `json:"outputs,omitempty"`
}

// reserved are the entry's own pins, which an arg of the same name does
// not get.
var reserved = []string{"settings", "connection", "caller", "cursor", "args"}

// EmitConfig is Emit connector event's config.
type EmitConfig struct {
	Contract string `json:"contract"`
	Event    string `json:"event"`
}

// Contribute registers the connector.op kind and the scripted connector
// nodes; deps returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	contract := apps.BuiltinContracts(reg)
	cognition.Kinds.Register(reg, cognition.Kind{
		Descriptor: extensions.Descriptor{ID: apps.KindConnectorOp, Source: source, Order: 200}, Title: "Connector operation",
		Events:      []string{apps.NodeOperation},
		Description: "Answers one operation of a scripted connector type an app declares (forge 07d): from the call's args, the connection's settings and the caller, the result (Return) or a typed error (Return error). It runs on the server as the app's own principal, so it reads and writes the app's records by commands. Named by the manifest's connectors; a watch graph instead gets the stored cursor and returns a new one, raising events with Emit connector event.",
		Returns:     "the operation's result fields, or a typed error",
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: apps.NodeOperation, Source: source, Order: 960}, Title: "Operation called", Category: "Events", Event: true,
		Kinds:       []string{apps.KindConnectorOp},
		Description: "Starts a scripted connector's operation graph when a call reaches it on the connector bus: one output per top-level field of the operation's args, the connection's settings (secrets included: the graph runs on the server and they never leave it), the connection's id and the caller's principal. In a watch graph: the stored cursor in place of the args.",
		Example:     "Operation called mail.transport@1 send, giving the message and the connection's mailbox setting",
		Examples:    cognition.ConfigExample(apps.NodeOperation, `{"contract":"test.echo@1","operation":"echo"}`),
		Tags:        []string{"connector", "scripted", "operation"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"contract":{"type":"string","title":"Contract","description":"The contract, id@version, one the connector names"},
			"operation":{"type":"string","title":"Operation","description":"The operation's id"},
			"watch":{"type":"boolean","title":"Watch","description":"The event source's graph: the cursor in, a new cursor out, events by Emit connector event"}}}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c OperationConfig
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			out := []cognition.Pin{cognition.ExecPin(cognition.Then, "")}
			if c.Watch {
				out = append(out, cognition.DataPin("cursor", "Cursor", "json", "Where the last pass stopped, as the graph returned it; nothing on the first pass"))
			} else {
				for _, p := range c.Outputs {
					p.Exec, p.Optional = false, false
					out = append(out, p)
				}
				out = append(out, cognition.DataPin("args", "Args", "json", "The call's args, whole"),
					cognition.DataPin("caller", "Caller", "json", "Who made the call: {kind, user, app, tenant}"))
			}
			out = append(out, cognition.DataPin("settings", "Settings", "json", "The connection's settings, secrets included"),
				cognition.DataPin("connection", "Connection", "string", "The connection's id; empty for a call with none"))
			return cognition.Pins{Out: out}, nil
		},
		Normalize: func(n cognition.Normalizing) (json.RawMessage, error) {
			var c OperationConfig
			if len(n.Config) > 0 {
				if err := json.Unmarshal(n.Config, &c); err != nil {
					return nil, err
				}
			}
			c.Outputs = nil
			if c.Watch || c.Contract == "" || c.Operation == "" {
				return json.Marshal(c)
			}
			ct, ok := contract(c.Contract)
			if !ok {
				return nil, fmt.Errorf("there is no contract %q a scripted connector can implement; list them with connectors.contracts", c.Contract)
			}
			op, ok := ct.Operation(c.Operation)
			if !ok {
				return nil, fmt.Errorf("contract %s has no operation %q", ct.Ref(), c.Operation)
			}
			fields, err := connectorsnodes.Fields(op.Args)
			if err != nil {
				return nil, fmt.Errorf("the args of %s cannot be read: %v", op.ID, err)
			}
			for _, f := range fields {
				if slices.Contains(reserved, f.Name) {
					continue // read it from args
				}
				c.Outputs = append(c.Outputs, cognition.Pin{ID: f.Name, Name: label(f.Name), Type: f.Type, Description: f.Description})
			}
			return json.Marshal(c)
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: apps.NodeReturnError, Source: source, Order: 961}, Title: "Return error", Category: "Flow",
		Kinds:       []string{apps.KindConnectorOp},
		Description: "Ends a scripted connector's operation with a typed error, as a builtin adapter would reply: auth, notFound, conflict, invalid, rateLimited, unavailable or notSupported. rateLimited and unavailable are retried by a job unless retryable is false.",
		Example:     "Return error notFound \"no such mailbox\" when the mailbox setting names none",
		Examples:    cognition.ConfigExample(apps.NodeReturnError, `{"code":"notFound"}`),
		Tags:        []string{"connector", "scripted", "error"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"code":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"title":"Code","description":"The error code when the Code input is unconnected"}}}`),
		Pins: cognition.Pins{In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
			{ID: "code", Name: "Code", Type: "string", Optional: true, Description: "The error code; the config's when unconnected"},
			cognition.DataPin("message", "Message", "string", "What went wrong, in words the caller can act on"),
			{ID: "retryable", Name: "Retryable", Type: "bool", Optional: true, Description: "Whether trying again may work; the code's default when unconnected"},
			{ID: "retryAfter", Name: "Retry after", Type: "number", Optional: true, Description: "Seconds to wait before trying again, for rateLimited"}}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Code string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			code, _ := cognition.Get[string](in, "code")
			if code == "" {
				code = c.Code
			}
			if !connectors.ValidCode(code) {
				return nil, "", fmt.Errorf("%q is not an error code: use one of %s", code, strings.Join(connectors.Codes, ", "))
			}
			msg, _ := cognition.Get[string](in, "message")
			e := connectors.Errorf(code, "%s", msg)  // the code's default retryable
			if v, ok := in["retryable"].(bool); ok { // absent when unconnected
				e.Retry = v
			}
			if v, ok := in["retryAfter"].(float64); ok && v > 0 {
				e.RetryAfterMs = int64(v * 1000)
			}
			x.Return(cognition.Values{ErrorKey: e})
			return nil, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodeEmit, Source: source, Order: 962}, Title: "Emit connector event", Category: "Connectors", Effects: true,
		Kinds:       []string{livenodes.KindAction, livenodes.KindTrigger, apps.KindConnectorOp},
		Description: "Raises an event of a contract on a connection of a scripted connector type the app declares, exactly as a builtin adapter's event source would: the payload is checked against the event's schema, and its id (from the event's id fields) makes an event raised twice start triggers once. Only in the graphs of the app that declares a type implementing the contract, for connections of that type.",
		Example:     "Emit connector event mail.mailbox@1 messageArrived on the stub's connection when the test server's record of a message is created",
		Examples:    cognition.ConfigExample(NodeEmit, `{"contract":"test.echo@1","event":"finished"}`),
		Tags:        []string{"connector", "scripted", "event"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"contract":{"type":"string","title":"Contract","description":"The contract, id@version"},
			"event":{"type":"string","title":"Event","description":"The event's id"}},"required":["contract","event"]}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("connection", "Connection", "string", "The connection, of one of the app's scripted types implementing the contract"),
				{ID: "payload", Name: "Payload", Type: cognition.Any, Description: "The event's payload, an object in the event's schema"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("id", "ID", "string", "The event's id")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c EmitConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			conn, _ := cognition.Get[string](in, "connection")
			payload, _ := json.Marshal(in["payload"])
			id, err := emit(x, deps(), contract, c, conn, payload)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"id": id}, cognition.Then, nil
		},
	})
}

// emit checks and raises one event: buffered for the event source in a
// watch graph's run, else published to CX_<tenant>.
func emit(x *cognition.Exec, d Deps, contract func(string) (connectors.Contract, bool), c EmitConfig, conn string, payload json.RawMessage) (string, error) {
	app, ok := strings.CutPrefix(x.Owner, "app:")
	if !ok {
		return "", fmt.Errorf("Emit connector event runs only in an app's own graphs")
	}
	ct, ok := contract(c.Contract)
	if !ok {
		return "", fmt.Errorf("there is no contract %q", c.Contract)
	}
	def, ok := ct.Event(c.Event)
	if !ok {
		return "", fmt.Errorf("contract %s has no event %q", ct.Ref(), c.Event)
	}
	if err := tools.CheckArgs(def.Payload, payload); err != nil {
		return "", fmt.Errorf("the payload of %s %s: %v", ct.Ref(), def.ID, err)
	}
	if conn == "" {
		return "", fmt.Errorf("Emit connector event needs a connection")
	}
	tenant := x.Principal.TenantID
	cn, found, err := connections.Get(x.Context(), d.Store, tenant, conn)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("there is no connection %s", conn)
	}
	typeApp, name, _ := strings.Cut(cn.Type, ".")
	in, running := installs.Running(x.Context(), d.Store, tenant, app)
	k, declared := in.Version.Connector(name)
	if !running || typeApp != app || !declared || !slices.Contains(k.Contracts, ct.Ref()) {
		return "", fmt.Errorf("connection %s is a %s, not one of the app %s's scripted types implementing %s", cn.Name, cn.Type, app, ct.Ref())
	}
	id := connectors.EventID(def, ct.Ref(), payload)
	if h, ok := x.Host.(*Host); ok && h.Watch {
		h.Events = append(h.Events, adapter.Event{Contract: ct.Ref(), Name: def.ID, ID: id, Payload: payload})
		return id, nil
	}
	if d.JS == nil {
		return "", fmt.Errorf("there is no event stream here")
	}
	return id, connectors.PublishEvent(x.Context(), d.JS, tenant, conn, ct.Ref(), def.ID, id, payload, timeNow())
}

func label(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
