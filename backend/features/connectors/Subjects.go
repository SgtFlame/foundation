package connectors

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Token is an id as one subject token: . replaced by _ (mail.imap is
// mail_imap). Contract, type and operation ids never hold _, so it is one
// to one.
func Token(id string) string { return strings.ReplaceAll(id, ".", "_") }

// FromToken is a token's id.
func FromToken(tok string) string { return strings.ReplaceAll(tok, "_", ".") }

// NoConnection is the connection token of a type that needs none.
const NoConnection = "_"

// CallSubject is a call: cx.call.<tenant>.<type>.<connection>.<operation>.
func CallSubject(tenant uint64, typ, connection, op string) string {
	if connection == "" {
		connection = NoConnection
	}
	return fmt.Sprintf("cx.call.%d.%s.%s.%s", tenant, Token(typ), connection, Token(op))
}

// CallSubjects are every call to typ, in every tenant: what an adapter
// instance subscribes to.
func CallSubjects(typ string) string { return fmt.Sprintf("cx.call.*.%s.>", Token(typ)) }

// TenantCallSubjects are the calls to typ in one tenant, for an adapter
// that serves some tenants only.
func TenantCallSubjects(tenant uint64, typ string) string {
	return fmt.Sprintf("cx.call.%d.%s.>", tenant, Token(typ))
}

// CallQueue is the queue group of a type's adapter instances: NATS gives
// each call to one.
func CallQueue(typ string) string { return "cx." + Token(typ) }

// ParsedCall is a call subject's parts.
type ParsedCall struct {
	Tenant     uint64
	Type       string
	Connection string // "" for none
	Operation  string
}

// ParseCallSubject splits a call subject.
func ParseCallSubject(subject string) (ParsedCall, error) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[0] != "cx" || parts[1] != "call" {
		return ParsedCall{}, fmt.Errorf("%q is not a call subject", subject)
	}
	var tenant uint64
	if _, err := fmt.Sscan(parts[2], &tenant); err != nil || tenant == 0 {
		return ParsedCall{}, fmt.Errorf("%q names no tenant", subject)
	}
	c := ParsedCall{Tenant: tenant, Type: FromToken(parts[3]), Connection: parts[4], Operation: FromToken(parts[5])}
	if c.Connection == NoConnection {
		c.Connection = ""
	}
	return c, nil
}

// EventSubject is a connector event in CX_<tenant> (07c):
// cx.event.<tenant>.<connection>.<contract>.<event>. The contract token is
// its id; its version is in the event's header.
func EventSubject(tenant uint64, connection, contract, event string) string {
	return fmt.Sprintf("cx.event.%d.%s.%s.%s", tenant, connection, Token(contract), Token(event))
}

// ChangedSubject says a connection changed, so adapters drop its sessions.
func ChangedSubject(tenant uint64, connection string) string {
	return fmt.Sprintf("cx.changed.%d.%s", tenant, connection)
}

// ChangedSubjects are every connection change.
const ChangedSubjects = "cx.changed.*.*"

// DiscoveredSubject says a connection's discovered contract changed.
func DiscoveredSubject(tenant uint64, connection string) string {
	return fmt.Sprintf("cx.discovered.%d.%s", tenant, connection)
}

// InSubject is reserved for the inbound direction (75a): a surface's
// request to the gateway. Nothing in 07b subscribes to cx.in.>.
func InSubject(tenant uint64, exposure, method string) string {
	return fmt.Sprintf("cx.in.%d.%s.%s", tenant, Token(exposure), Token(method))
}

// InNotifySubject is reserved for 75a: what the gateway pushes to a
// surface's session.
func InNotifySubject(session string) string { return "cx.in.notify." + session }

// KV buckets.
const (
	// AdaptersBucket holds announcements: <type>.<instance> and
	// surface.<surface>.<instance>, refreshed by each adapter instance.
	AdaptersBucket = "cx_adapters"
	// HooksBucket maps a callback's hook id to its tenant, connection and
	// type (26b): <hookId> → Hook.
	HooksBucket = "cx_hooks"
)

// DiscoveredBucket holds a tenant's discovered contracts, by connection.
func DiscoveredBucket(tenant uint64) string { return fmt.Sprintf("cx_discovered_%d", tenant) }

// Hook is a cx_hooks value (26b).
type Hook struct {
	Tenant     uint64 `json:"tenant,string"`
	Connection string `json:"connection"`
	Type       string `json:"type"`
}

// Headers of a call request.
const (
	HeaderCall      = "Cx-Call"      // the call id, a TSID
	HeaderContract  = "Cx-Contract"  // id@version
	HeaderPrincipal = "Cx-Principal" // features.PrincipalRef as JSON
	HeaderMeta      = "Cx-Meta"      // features.Meta as JSON
	HeaderTrace     = "Cx-Trace"     // the trace id
	HeaderDeadline  = "Cx-Deadline"  // RFC 3339 with nanoseconds
	HeaderKey       = "Cx-Key"       // the idempotency key, when the operation has one
	HeaderFiles     = "Cx-Files"     // file ids the dispatcher vouches the caller may use, comma separated (forge 07n)
)

// ReceiveSubject is a callback a hook received for a connection (26b-1):
// cx.receive.<tenant>.<type>.<connection>, answered by the type's adapter,
// which verifies it, publishes its events and replies.
func ReceiveSubject(tenant uint64, typ, connection string) string {
	return fmt.Sprintf("cx.receive.%d.%s.%s", tenant, Token(typ), connection)
}

// ReceiveSubjects are every callback for typ, in every tenant.
func ReceiveSubjects(typ string) string { return fmt.Sprintf("cx.receive.*.%s.*", Token(typ)) }

// TenantReceiveSubjects are the callbacks for typ in one tenant.
func TenantReceiveSubjects(tenant uint64, typ string) string {
	return fmt.Sprintf("cx.receive.%d.%s.*", tenant, Token(typ))
}

// ParseReceiveSubject splits a receive subject into its tenant, type and
// connection.
func ParseReceiveSubject(subject string) (uint64, string, string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 || parts[0] != "cx" || parts[1] != "receive" {
		return 0, "", "", fmt.Errorf("%q is not a receive subject", subject)
	}
	var tenant uint64
	if _, err := fmt.Sscan(parts[2], &tenant); err != nil || tenant == 0 {
		return 0, "", "", fmt.Errorf("%q names no tenant", subject)
	}
	return tenant, FromToken(parts[3]), parts[4], nil
}

// RegisterSubject asks a connection's type to register, update or remove
// its hook with the service (26b-2): cx.register.<tenant>.<type>.<connection>,
// answered by the type's adapter with a Registrar.
func RegisterSubject(tenant uint64, typ, connection string) string {
	return fmt.Sprintf("cx.register.%d.%s.%s", tenant, Token(typ), connection)
}

// RegisterSubjects are every registration for typ, in every tenant.
func RegisterSubjects(typ string) string { return fmt.Sprintf("cx.register.*.%s.*", Token(typ)) }

// TenantRegisterSubjects are the registrations for typ in one tenant.
func TenantRegisterSubjects(tenant uint64, typ string) string {
	return fmt.Sprintf("cx.register.%d.%s.*", tenant, Token(typ))
}

// ParseRegisterSubject splits a register subject.
func ParseRegisterSubject(subject string) (uint64, string, string, error) {
	return ParseReceiveSubject(strings.Replace(subject, "cx.register.", "cx.receive.", 1))
}

// Hook registration ops.
const (
	HookRegister   = "register"
	HookUpdate     = "update"
	HookUnregister = "unregister"
)

// HookRegistration is what a type's Registrar keeps of a hook it
// registered with the service: the service's id for it and any data it
// needs to change it.
type HookRegistration struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data,omitempty"`
}

// RegisterRequest is a cx.register body.
type RegisterRequest struct {
	Op           string            `json:"op"` // register, update or unregister
	URL          string            `json:"url,omitempty"`
	Events       []string          `json:"events,omitempty"` // the listened events' subscription names
	Registration *HookRegistration `json:"registration,omitempty"`
}

// RegisterReply is a cx.register reply: the registration (none after
// unregister) and the signing secret the service gave, if any.
type RegisterReply struct {
	Registration *HookRegistration `json:"registration,omitempty"`
	Secret       string            `json:"secret,omitempty"`
	Error        *Error            `json:"error,omitempty"`
}
