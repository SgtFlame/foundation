// Package connectorsnodes is the connector nodes (forge 07b): Call
// connector, which calls any contract operation through the dispatcher,
// and On connector event, an app.trigger event node for a contract's
// events. Their pins follow from the contract's schemas: Normalize copies
// them into the node's config when it is added or changed.
package connectorsnodes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
)

// Node types.
const (
	Call = "connector.call"
	On   = "connector.on"
)

// Failed is Call connector's exec output for a typed error (its data
// output error holds the error).
const Failed = "failed"

const source = "connectors"

// CallConfig is Call connector's config: what the author picks (contract,
// operation) and what Normalize fills from the contract.
type CallConfig struct {
	Contract  string `json:"contract"` // id@version, or conn.<connection>
	Operation string `json:"operation"`
	// CatchRetryable leaves by Error on a retryable error too; otherwise
	// it fails the run, so a job retries it.
	CatchRetryable bool `json:"catchRetryable,omitempty"`
	// Filled by Normalize:
	Inputs     []cognition.Pin `json:"inputs,omitempty"`
	Outputs    []cognition.Pin `json:"outputs,omitempty"`
	Connection bool            `json:"connection,omitempty"` // a connection input
	Effect     string          `json:"effect,omitempty"`
	Sample     map[string]any  `json:"sample,omitempty"` // the result example, for stubs
	Title      string          `json:"title,omitempty"`
}

// OnConfig is On connector event's config.
type OnConfig struct {
	Contract   string `json:"contract"`
	Event      string `json:"event"`
	Connection string `json:"connection,omitempty"` // only this connection's events
	// Filled by Normalize:
	Outputs []cognition.Pin `json:"outputs,omitempty"`
}

// Field is one top-level field of an object schema, in the schema's order.
type Field struct {
	Name, Type, Description string
	Required                bool
}

// Fields are an object schema's top-level fields in their written order
// (07b question 1: a nested object is one pin).
func Fields(schema json.RawMessage) ([]Field, error) {
	var s struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, err
	}
	if len(s.Properties) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(s.Properties))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	var out []Field
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		var p struct {
			Type        any    `json:"type"`
			Description string `json:"description"`
		}
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		out = append(out, Field{Name: name, Type: PinType(p.Type), Description: p.Description, Required: required[name]})
	}
	return out, nil
}

// PinType is a JSON Schema type as a pin's: string, number, bool, else json.
func PinType(t any) string {
	switch t {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "bool"
	}
	return "json"
}

func label(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// Deps are the nodes' dependencies, made after the registry.
type Deps = connectors.Deps

// callPins are Call connector's pins for its normalized config.
func callPins(config json.RawMessage) (cognition.Pins, error) {
	var c CallConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &c); err != nil {
			return cognition.Pins{}, err
		}
	}
	in := []cognition.Pin{cognition.ExecPin(cognition.Then, "")}
	if c.Connection {
		in = append(in, cognition.Pin{ID: "connection", Name: "Connection", Type: "string", Optional: true,
			Description: "The connection to call, whose type implements the contract; empty uses a type that needs none"})
	}
	for _, p := range c.Inputs {
		p.Exec = false
		in = append(in, p)
	}
	out := []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.ExecPin(Failed, "Error")}
	for _, p := range c.Outputs {
		p.Exec, p.Optional = false, false
		out = append(out, p)
	}
	out = append(out, cognition.DataPin("error", "Error", "json", "The typed error when it leaves by Error: {code, message, retryable, retryAfter}"))
	return cognition.Pins{In: in, Out: out}, nil
}

// Contribute registers the connector nodes.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: Call, Source: source, Order: 950}, Title: "Call connector", Category: "Connectors",
		Effects: true, External: true,
		Description: "Calls an operation of a connector contract (a mail server, an API) through the connector bus: the connection's type implements it, served by an adapter in this server or another. Its inputs are the operation's args and its outputs the result's fields, from the contract's schemas. A typed error (auth, notFound, conflict, invalid, rateLimited, unavailable, notSupported) leaves by Error; a retryable one fails the run instead, so a job retries it, unless Catch retryable is on.",
		Example:     "Call connector mail.transport@1 send with the account's SMTP connection and the composed message, in the outbox job",
		Examples:    cognition.ConfigExample(Call, `{"contract":"mail.transport@1","operation":"send"}`),
		Tags:        []string{"connector", "integration", "external", "api"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"contract":{"type":"string","title":"Contract","description":"The contract, id@version (connectors.contracts), or conn.<connection> for a discovered one"},
			"operation":{"type":"string","title":"Operation","description":"The operation's id"},
			"catchRetryable":{"type":"boolean","title":"Catch retryable","description":"Leave by Error on a retryable error too, instead of failing the run for a job to retry"}},"required":["contract","operation"]}`),
		PinsFor: callPins,
		Normalize: func(n cognition.Normalizing) (json.RawMessage, error) {
			var c CallConfig
			if len(n.Config) > 0 {
				if err := json.Unmarshal(n.Config, &c); err != nil {
					return nil, err
				}
			}
			c.Inputs, c.Outputs, c.Connection, c.Effect, c.Sample, c.Title = nil, nil, false, "", nil, ""
			if c.Contract == "" {
				return json.Marshal(c)
			}
			d := deps()
			if d.Registry == nil {
				return nil, fmt.Errorf("there is no connector registry")
			}
			info, ok, err := d.Registry.Contract(n.Context(), n.Principal.TenantID, c.Contract)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("there is no contract %q; list them with connectors.contracts", c.Contract)
			}
			c.Contract = info.Ref
			if info.Source == connectors.FromDiscovered {
				c.Contract = info.Contract.ID
			}
			c.Connection = info.NeedsConnection && info.Source != connectors.FromDiscovered
			if c.Operation == "" {
				return json.Marshal(c)
			}
			op, ok := info.Contract.Operation(c.Operation)
			if !ok {
				return nil, fmt.Errorf("contract %s has no operation %q", info.Ref, c.Operation)
			}
			args, err := Fields(op.Args)
			if err != nil {
				return nil, fmt.Errorf("the args of %s cannot be read: %v", op.ID, err)
			}
			for _, f := range args {
				c.Inputs = append(c.Inputs, cognition.Pin{ID: f.Name, Name: label(f.Name), Type: f.Type, Optional: !f.Required, Description: f.Description})
			}
			result, err := Fields(op.Result)
			if err != nil {
				return nil, fmt.Errorf("the result of %s cannot be read: %v", op.ID, err)
			}
			for _, f := range result {
				if f.Name == "error" {
					continue // the Error pin's
				}
				c.Outputs = append(c.Outputs, cognition.Pin{ID: f.Name, Name: label(f.Name), Type: f.Type, Description: f.Description})
			}
			c.Effect, c.Title = op.Effect, op.Title
			if len(op.Examples) > 0 {
				_ = json.Unmarshal(op.Examples[0].Result, &c.Sample)
			}
			return json.Marshal(c)
		},
		SampleFor: func(config json.RawMessage) cognition.Values {
			var c CallConfig
			_ = json.Unmarshal(config, &c)
			return cognition.Values(c.Sample)
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c CallConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			d := deps()
			if d.Dispatcher == nil {
				return nil, "", fmt.Errorf("there is no connector bus here")
			}
			args := map[string]any{}
			for _, p := range c.Inputs {
				if v, ok := in[p.ID]; ok && v != nil {
					args[p.ID] = v
				}
			}
			raw, _ := json.Marshal(args)
			conn, _ := cognition.Get[string](in, "connection")
			result, e := d.Dispatcher.Call(x.ServiceArgs, connectors.Call{Contract: c.Contract, Operation: c.Operation, Connection: conn, Args: raw})
			if e != nil {
				if e.Retryable() && !c.CatchRetryable {
					return nil, "", e
				}
				var ev map[string]any
				b, _ := json.Marshal(e)
				_ = json.Unmarshal(b, &ev)
				return cognition.Values{"error": ev}, Failed, nil
			}
			var res map[string]any
			_ = json.Unmarshal(result, &res)
			out := cognition.Values{}
			for _, p := range c.Outputs {
				out[p.ID] = res[p.ID]
			}
			return out, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: On, Source: source, Order: 951}, Title: "On connector event", Category: "Events", Event: true,
		Kinds:       []string{livenodes.KindTrigger},
		Description: "Starts an app's trigger when a connection reports an event of a contract (a new message, a payment), polled by an adapter or pushed to a hook; its outputs are the event's payload fields and the connection. Run by the job runner from the tenant's connector event stream.",
		Example:     "On connector event mail.mailbox@1 messageArrived, to ingest each new message",
		Examples:    cognition.ConfigExample(On, `{"contract":"mail.mailbox@1","event":"messageArrived"}`),
		Tags:        []string{"connector", "trigger", "event"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"contract":{"type":"string","title":"Contract","description":"The contract, id@version"},
			"event":{"type":"string","title":"Event","description":"The event's id"},
			"connection":{"type":"string","title":"Connection","description":"Only this connection's events; every connection's when empty"}},"required":["contract","event"]}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c OnConfig
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			out := []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("connection", "Connection", "string", "The connection that reported it"),
				cognition.DataPin("event", "Event", "json", "The whole event: {contract, name, id, payload, at}")}
			for _, p := range c.Outputs {
				p.Exec, p.Optional = false, false
				out = append(out, p)
			}
			return cognition.Pins{Out: out}, nil
		},
		Normalize: func(n cognition.Normalizing) (json.RawMessage, error) {
			var c OnConfig
			if len(n.Config) > 0 {
				if err := json.Unmarshal(n.Config, &c); err != nil {
					return nil, err
				}
			}
			c.Outputs = nil
			if c.Contract == "" || c.Event == "" {
				return json.Marshal(c)
			}
			d := deps()
			if d.Registry == nil {
				return nil, fmt.Errorf("there is no connector registry")
			}
			info, ok, err := d.Registry.Contract(n.Context(), n.Principal.TenantID, c.Contract)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("there is no contract %q; list them with connectors.contracts", c.Contract)
			}
			c.Contract = info.Ref
			e, ok := info.Contract.Event(c.Event)
			if !ok {
				return nil, fmt.Errorf("contract %s has no event %q", info.Ref, c.Event)
			}
			fields, err := Fields(e.Payload)
			if err != nil {
				return nil, fmt.Errorf("the payload of %s cannot be read: %v", e.ID, err)
			}
			for _, f := range fields {
				if f.Name == "connection" || f.Name == "event" {
					continue
				}
				c.Outputs = append(c.Outputs, cognition.Pin{ID: f.Name, Name: label(f.Name), Type: f.Type, Description: f.Description})
			}
			return json.Marshal(c)
		},
	})
}
