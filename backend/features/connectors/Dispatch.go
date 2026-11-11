package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/nats-io/nats.go"
)

// Connections resolves a connection's type for a call as the caller (07c
// builds them: settings and secrets in SecretStore, use checked as the
// run's principal). Until then a call that names a connection needs one
// given to the Dispatcher.
type Connections interface {
	TypeOf(args features.ServiceArgs, connection string) (string, error)
}

// ConnectionsFunc is a Connections from a function.
type ConnectionsFunc func(args features.ServiceArgs, connection string) (string, error)

// TypeOf calls f.
func (f ConnectionsFunc) TypeOf(args features.ServiceArgs, connection string) (string, error) {
	return f(args, connection)
}

// DefaultTimeout is how long a call waits for its reply when its context
// has no earlier deadline.
const DefaultTimeout = 30 * time.Second

// Dispatcher is the caller's side of the connector bus: it resolves the
// connection's type, sends the request on the bus and reads the reply.
// It holds no state, so any process may call any connector; it never
// retries a call itself (a job does, 07a).
type Dispatcher struct {
	NC          *nats.Conn
	Registry    *Registry
	Connections Connections
	Timeout     time.Duration
}

// Call is one call through the dispatcher.
type Call struct {
	Contract   string          // id@version, an id (its latest), or conn.<connection>
	Operation  string          // the operation's id
	Connection string          // the connection; empty for a contract whose types need none
	Args       json.RawMessage // the operation's args, an object
}

// Call sends a call and returns its result or a typed error. A call no
// adapter serves is unavailable at once, and so is one past its deadline;
// both are retryable.
func (d *Dispatcher) Call(args features.ServiceArgs, c Call) (json.RawMessage, *Error) {
	ctx := args.Context()
	tenant := args.Principal.TenantID
	info, ok, err := d.Registry.Contract(ctx, tenant, c.Contract)
	if err != nil {
		return nil, &Error{Code: CodeInvalid, Message: err.Error()}
	}
	if !ok {
		return nil, Errorf(CodeNotSupported, "there is no contract %s", c.Contract)
	}
	op, ok := info.Contract.Operation(c.Operation)
	if !ok {
		return nil, Errorf(CodeNotSupported, "contract %s has no operation %q", info.Ref, c.Operation)
	}
	if len(c.Args) == 0 {
		c.Args = json.RawMessage(`{}`)
	}
	if err := tools.CheckArgs(op.Args, c.Args); err != nil {
		return nil, Errorf(CodeInvalid, "%s %s: %v", info.Ref, op.ID, err)
	}
	typ, conn, e := d.typeFor(args, info, c.Connection)
	if e != nil {
		return nil, e
	}
	h := CallHeaders{CallID: strconv.FormatUint(features.NewTSID(), 10), Contract: info.Ref, Principal: args.Principal.Ref(),
		Meta: args.Meta, Trace: logging.TraceFrom(ctx)}
	if op.Key != "" {
		var a map[string]any
		_ = json.Unmarshal(c.Args, &a)
		if v, ok := a[op.Key]; ok && v != nil {
			h.Key = fmt.Sprint(v)
		}
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	h.Deadline, _ = ctx.Deadline()
	// A discovered operation goes by its id; the adapter sends its remote name
	msg := nats.NewMsg(CallSubject(tenant, typ, conn, op.ID))
	h.Set(msg.Header)
	msg.Data = c.Args
	reply, err := d.NC.RequestMsgWithContext(ctx, msg)
	switch {
	case errors.Is(err, nats.ErrNoResponders):
		return nil, Errorf(CodeUnavailable, "no adapter serves %s right now", typ)
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout):
		return nil, Errorf(CodeUnavailable, "%s did not answer %s in time", typ, op.ID)
	case err != nil:
		return nil, Errorf(CodeUnavailable, "the call to %s failed: %v", typ, err)
	}
	var r Reply
	if err := json.Unmarshal(reply.Data, &r); err != nil {
		return nil, Errorf(CodeUnavailable, "%s answered %s with a reply that cannot be read", typ, op.ID)
	}
	if r.Error != nil {
		if !ValidCode(r.Error.Code) {
			r.Error.Code = CodeUnavailable
		}
		return nil, r.Error
	}
	if len(r.Result) == 0 {
		r.Result = json.RawMessage(`{}`)
	}
	return r.Result, nil
}

// typeFor is the type a call goes to and its connection token: the
// connection's type, or the type implementing the contract that needs no
// connection (an online one first).
func (d *Dispatcher) typeFor(args features.ServiceArgs, info ContractInfo, connection string) (string, string, *Error) {
	if info.Source == FromDiscovered {
		connection = strings.TrimPrefix(info.Contract.ID, DiscoveredPrefix)
	}
	if connection != "" {
		if d.Connections == nil {
			return "", "", Errorf(CodeNotSupported, "connection %s cannot be resolved: connections are not available here", connection)
		}
		typ, err := d.Connections.TypeOf(args, connection)
		if err != nil {
			if e := (*Error)(nil); errors.As(err, &e) {
				return "", "", e
			}
			return "", "", Errorf(CodeNotFound, "%v", err)
		}
		if info.Source != FromDiscovered {
			t, ok, _ := d.Registry.Type(args.Context(), args.Principal.TenantID, typ)
			if ok && !implements(t.Type, info.Ref) {
				return "", "", Errorf(CodeInvalid, "connection %s is a %s, which does not implement %s", connection, typ, info.Ref)
			}
		}
		return typ, connection, nil
	}
	cat, err := d.Registry.Catalog(args.Context(), args.Principal.TenantID)
	if err != nil {
		return "", "", Errorf(CodeUnavailable, "%v", err)
	}
	var pick *TypeInfo
	needs := false
	for i, t := range cat.Types {
		if !implements(t.Type, info.Ref) {
			continue
		}
		if !t.Type.NoConnection {
			needs = true
			continue
		}
		if pick == nil || (t.Online && !pick.Online) {
			pick = &cat.Types[i]
		}
	}
	switch {
	case pick != nil:
		return pick.Type.ID, NoConnection, nil
	case needs:
		return "", "", Errorf(CodeInvalid, "%s needs a connection", info.Ref)
	}
	return "", "", Errorf(CodeNotSupported, "no connector type implements %s", info.Ref)
}

func implements(t Type, ref string) bool {
	for _, c := range t.Contracts {
		if c == ref {
			return true
		}
	}
	return false
}
