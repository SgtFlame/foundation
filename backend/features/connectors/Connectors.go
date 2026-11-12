// Package connectors is services outside the tenant described by metadata
// (forge 07b, documentation/Connectors.md): contracts (named, versioned
// APIs of operations and events), connector types that implement them,
// and the connector bus, NATS request/reply, on which every call travels
// to a connector adapter that may run in this process or in another.
//
//   - The metadata (Contract, Type, SurfaceInfo) is known to every
//     process, and shaped so every operation can be offered as a tool as
//     it is (ToolSpec, MCPTool, FunctionSpec, OpenAPI).
//   - Builtin contracts and types are contributed to the connector.contract
//     and connector.type points; adapters announce what they serve in KV
//     cx_adapters, and the Registry merges both, per tenant.
//   - The Dispatcher is the caller's side of the bus; package adapter is
//     the adapter runtime, the same Go code in-process and external.
package connectors

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("connectors")

// Contracts is the connector.contract point: builtin contracts.
var Contracts = extensions.NewPoint[BuiltinContract]("connector.contract",
	"Builtin connector contracts: named, versioned APIs of operations and events, described by metadata (forge 07b)")

// Types is the connector.type point: builtin connector types, served by an
// adapter in this process or another.
var Types = extensions.NewPoint[BuiltinType]("connector.type",
	"Builtin connector types: implementations of contracts, served on the connector bus by an adapter (forge 07b)")

// BuiltinContract is a connector.contract contribution; its Descriptor's
// ID is the contract's reference, id@version.
type BuiltinContract struct {
	extensions.Descriptor
	Contract Contract
}

// BuiltinType is a connector.type contribution; its Descriptor's ID is
// the type's.
type BuiltinType struct {
	extensions.Descriptor
	Type Type
}

// ContributeBuiltins registers builtin contracts and types from source (a
// feature's Contribute calls it).
func ContributeBuiltins(reg *extensions.Registry, source string, contracts []Contract, types []Type) {
	for _, c := range contracts {
		Contracts.Register(reg, BuiltinContract{Descriptor: extensions.Descriptor{ID: c.Ref(), Source: source}, Contract: c})
	}
	for _, t := range types {
		if t.Source == "" {
			t.Source = SourceBuiltin
		}
		Types.Register(reg, BuiltinType{Descriptor: extensions.Descriptor{ID: t.ID, Source: source}, Type: t})
	}
}

// Meta is what every contract, type and surface carries for people and
// models (Architecture rule 4).
type Meta struct {
	Title       string   `json:"title"`
	Description string   `json:"description"` // for a model: what it is and when to use it
	Tags        []string `json:"tags"`
	Icon        string   `json:"icon,omitempty"`
}

// Effects an operation has: what a tool gate and the audit treat it as.
const (
	EffectRead  = "read"
	EffectWrite = "write"
	EffectSend  = "send"
)

// Delivery is how an event arrives.
const (
	DeliveryWatch    = "watch"    // an adapter's Watch produces it
	DeliveryCallback = "callback" // the service pushes it to Axiom's hook URL (15f)
)

// Directions of types and surfaces.
const (
	Outbound = "outbound" // a type: serves cx.call requests by calling the outside service
	Inbound  = "inbound"  // a surface: turns what arrives outside into cx.in requests (15a)
)

// Sources of a type.
const (
	SourceBuiltin  = "builtin"  // Go, served by an adapter
	SourceScripted = "scripted" // blueprints (07d)
)

// Contract is a named, versioned API: mail.mailbox@1. A type implements
// one version of each contract it names; an incompatible change is a new
// version beside the old (07b question 2).
type Contract struct {
	ID         string      `json:"id"`
	Version    int         `json:"version"`
	Meta       Meta        `json:"meta"`
	Operations []Operation `json:"operations"`
	Events     []EventDef  `json:"events,omitempty"`
}

// Ref is the contract's reference, id@version.
func (c Contract) Ref() string { return Ref(c.ID, c.Version) }

// Operation finds an operation by id.
func (c Contract) Operation(id string) (Operation, bool) {
	for _, op := range c.Operations {
		if op.ID == id {
			return op, true
		}
	}
	return Operation{}, false
}

// Event finds an event by id.
func (c Contract) Event(id string) (EventDef, bool) {
	for _, e := range c.Events {
		if e.ID == id {
			return e, true
		}
	}
	return EventDef{}, false
}

// Operation is one call of a contract, shaped so it can be offered as a
// tool as it is.
type Operation struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Args        json.RawMessage `json:"args"`   // a JSON Schema, type object, in the tool subset
	Result      json.RawMessage `json:"result"` // a JSON Schema, type object, in the tool subset
	Examples    []Example       `json:"examples"`
	Effect      string          `json:"effect"` // read, write or send
	// Destructive marks a write that deletes or overwrites (MCP's
	// destructiveHint; 07b question 8).
	Destructive bool `json:"destructive,omitempty"`
	// Key is the arg that makes the operation idempotent (a message id), a
	// top-level field or a dotted path to a nested one (message.messageId):
	// a call with a key already done is not done again (the ledger, 07c).
	Key string `json:"key,omitempty"`
	// Errors are the error codes it may return.
	Errors []string `json:"errors,omitempty"`
	// Callbacks are the events that answer it later (an export's ready).
	Callbacks []CallbackDef `json:"callbacks,omitempty"`
	// Remote is the outside name a discovered operation was found under,
	// which the adapter sends (15c).
	Remote string `json:"remote,omitempty"`
}

// Example is one call: args and the result they give.
type Example struct {
	Title  string          `json:"title,omitempty"`
	Args   json.RawMessage `json:"args"`
	Result json.RawMessage `json:"result"`
}

// CallbackDef is an event that answers an operation later, matched by a
// correlation key: Result names a field of the operation's result and
// Event a field of the event's payload holding the same value.
type CallbackDef struct {
	Event     string    `json:"event"`
	Correlate Correlate `json:"correlate"`
}

// Correlate names the fields that hold one correlation key, as dotted
// paths.
type Correlate struct {
	Result string `json:"result"`
	Event  string `json:"event"`
}

// EventDef is something a connection reports.
type EventDef struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Payload     json.RawMessage `json:"payload"` // a JSON Schema, type object, in the tool subset
	Example     json.RawMessage `json:"example"`
	// IDFields are the payload fields that make an event unique, joined as
	// its Nats-Msg-Id, so one pushed and polled is kept once.
	IDFields []string `json:"idFields"`
	// Delivery is watch, callback or both.
	Delivery []string `json:"delivery"`
	// Subscription is the service's own name for it (Stripe's
	// payment_intent.succeeded), which a Registrar asks for.
	Subscription string `json:"subscription,omitempty"`
}

// Type is an implementation of contracts: mail.imap.
type Type struct {
	ID        string   `json:"id"`
	Contracts []string `json:"contracts"` // id@version, one version each
	Meta      Meta     `json:"meta"`
	// Settings is the connection's settings schema, in Configuration's
	// schema language; fields marked secret go to SecretStore (07c).
	Settings     json.RawMessage `json:"settings,omitempty"`
	NoConnection bool            `json:"noConnection,omitempty"`
	Source       string          `json:"source"` // builtin or scripted
	// Discovers is set when the type's contract comes from each
	// connection (an MCP server's tools, an OpenAPI document): conn.<id>.
	Discovers bool `json:"discovers,omitempty"`
	// Receives is the callback verification scheme (stripe, github,
	// standard-webhooks, hmac-sha256), when the service pushes events.
	Receives string `json:"receives,omitempty"`
	// Exclusive is set when the outside service allows one session at a
	// time on a connection (POP3 locks the mailbox): its calls and its
	// event source take the connection's lock in cx_locks_<tenant> (07c).
	Exclusive bool `json:"exclusive,omitempty"`
}

// SurfaceInfo is an inbound surface as the registry lists it (15a): an
// outside endpoint that calls the gateway on the bus.
type SurfaceInfo struct {
	ID   string `json:"id"`
	Meta Meta   `json:"meta"`
}

// Receivers are the known callback verification schemes.
var Receivers = []string{"stripe", "github", "standard-webhooks", "hmac-sha256"}

// Ref is a contract reference, id@version.
func Ref(id string, version int) string { return id + "@" + strconv.Itoa(version) }

// ParseRef splits id@version; version is 0 when it names none (the latest).
func ParseRef(ref string) (id string, version int, err error) {
	id, v, ok := strings.Cut(ref, "@")
	if !ok {
		return id, 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return "", 0, fmt.Errorf("%q is not a contract reference: write id@version, such as mail.mailbox@1", ref)
	}
	return id, n, nil
}

// DiscoveredPrefix makes a discovered contract's id from a connection's:
// conn.<connection>.
const DiscoveredPrefix = "conn."

// DiscoveredID is the id of a connection's discovered contract.
func DiscoveredID(connection string) string { return DiscoveredPrefix + connection }
