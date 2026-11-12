package connections

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/nats-io/nats.go"
)

// Connections is the dispatcher's view of connections: a call names a
// connection the run's principal may use, in a state that can be called.
type Connections struct{ Deps func() Deps }

type testingKey struct{}

// testing marks a context whose call tests a connection: an errored one
// may be called.
func testing(ctx context.Context) context.Context { return context.WithValue(ctx, testingKey{}, true) }

// TypeOf is the connection's type, after checking use on it as the
// principal (agents included) and that it is neither disabled nor
// errored. Adapters trust this check and do not repeat it.
func (s Connections) TypeOf(args features.ServiceArgs, connection string) (string, error) {
	c, ok, err := Get(args.Context(), s.Deps().Projections, args.Principal.TenantID, connection)
	if err != nil {
		return "", connectors.Errorf(connectors.CodeUnavailable, "connection %s cannot be read now", connection)
	}
	if !ok {
		return "", connectors.Errorf(connectors.CodeNotFound, "there is no connection %s", connection)
	}
	if err := CheckAccess(args, c, "use"); err != nil {
		return "", connectors.Errorf(connectors.CodeAuth, "you may not use connection %s", c.Name)
	}
	testing, _ := args.Context().Value(testingKey{}).(bool)
	if why := Usable(c); why != "" && !(testing && c.Status == StatusError) {
		return "", connectors.Errorf(connectors.CodeUnavailable, "%s", why).NotRetryable()
	}
	return c.Type, nil
}

// Failed records a call's typed error: auth commits the error status,
// which stops the connection's event source until it is updated; an
// unavailable error is kept as its last error.
func (s Connections) Failed(args features.ServiceArgs, connection string, e *connectors.Error) {
	switch e.Code {
	case connectors.CodeAuth:
		SetStatus(args.Context(), s.Deps(), args.Principal.TenantID, connection, StatusError, e.Message)
	case connectors.CodeUnavailable, connectors.CodeRateLimited:
		SetStatus(args.Context(), s.Deps(), args.Principal.TenantID, connection, StatusOK, e.Code+": "+e.Message)
	}
}

// SetStatus commits a connection's status as the tenant's service
// principal, when it differs from what is recorded.
func SetStatus(ctx context.Context, d Deps, tenant uint64, connection, status, message string) {
	c, ok, err := Get(ctx, d.Projections, tenant, connection)
	if err != nil || !ok || c.Status == StatusDisabled || (c.Status == status && c.LastError == message) {
		return
	}
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
	raw, _ := json.Marshal(map[string]any{"status": status, "error": message})
	res := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "status-" + strconv.FormatUint(features.NewTSID(), 10), Type: "connection.setStatus", Target: connection, Args: raw})
	if res.Common.Code >= 300 {
		log.With("connection", connection).Warn(ctx, "the status of connection %s could not be recorded: %s", connection, res.Common.Message)
	}
}

// Serve answers adapters on cx.resolve (a connection's settings with its
// secrets, only to an adapter of its type) and cx.status (an event
// source's auth error), in a queue group in every server process, until
// ctx ends. Until authorization between processes is built, this channel
// is as trusted as the cluster.
func Serve(ctx context.Context, nc *nats.Conn, deps func() Deps) error {
	resolve, err := nc.QueueSubscribe(connectors.ResolveSubjects, connectors.ResolveQueue, func(m *nats.Msg) {
		reply := resolveReply(ctx, deps(), m)
		raw, _ := json.Marshal(reply)
		_ = m.Respond(raw)
	})
	if err != nil {
		return err
	}
	status, err := nc.QueueSubscribe(connectors.StatusSubjects, connectors.ResolveQueue, func(m *nats.Msg) {
		tenant, connection, ok := parse(m.Subject)
		var r connectors.StatusReport
		if ok && json.Unmarshal(m.Data, &r) == nil && r.Error.Code == connectors.CodeAuth {
			SetStatus(ctx, deps(), tenant, connection, StatusError, r.Error.Message)
		}
		_ = m.Respond([]byte(`{}`))
	})
	if err != nil {
		_ = resolve.Unsubscribe()
		return err
	}
	if err := nc.Flush(); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = resolve.Unsubscribe()
		_ = status.Unsubscribe()
	}()
	return nil
}

// parse splits cx.resolve.<tenant>.<connection> and cx.status's.
func parse(subject string) (uint64, string, bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 4 {
		return 0, "", false
	}
	tenant, err := strconv.ParseUint(parts[2], 10, 64)
	return tenant, parts[3], err == nil && tenant != 0
}

func resolveReply(ctx context.Context, d Deps, m *nats.Msg) connectors.ResolveReply {
	tenant, connection, ok := parse(m.Subject)
	var req connectors.ResolveRequest
	if !ok || json.Unmarshal(m.Data, &req) != nil {
		return connectors.ResolveReply{Error: connectors.Errorf(connectors.CodeInvalid, "a resolve request names a tenant, a connection and a type")}
	}
	c, found, err := Get(ctx, d.Projections, tenant, connection)
	switch {
	case err != nil:
		return connectors.ResolveReply{Error: connectors.Errorf(connectors.CodeUnavailable, "connection %s cannot be read now", connection)}
	case !found:
		return connectors.ResolveReply{Error: connectors.Errorf(connectors.CodeNotFound, "there is no connection %s", connection)}
	case c.Type != req.Type:
		return connectors.ResolveReply{Error: connectors.Errorf(connectors.CodeNotFound, "connection %s is not a %s", connection, req.Type)}
	}
	settings, err := Settings(ctx, d, tenant, c)
	if err != nil {
		log.With("connection", connection).Error(ctx, err, "the settings of connection %s cannot be resolved", connection)
		return connectors.ResolveReply{Error: connectors.Errorf(connectors.CodeUnavailable, "the settings of connection %s cannot be read now", connection)}
	}
	return connectors.ResolveReply{Type: c.Type, Settings: settings}
}

// TestResult is connection.test's result.
type TestResult struct {
	OK    bool              `json:"ok"`
	Error *connectors.Error `json:"error,omitempty"`
}

// CheckOperation is the operation every connector type answers through a
// contract it implements: a login and a no-op.
const CheckOperation = "check"

// Test sends the connection's check operation on the bus as the
// principal, and records its status: ok, or the typed error (unavailable
// when no adapter serves the type).
func Test(args features.ServiceArgs, d Deps, id string) (TestResult, error) {
	c, ok, err := Get(args.Context(), d.Projections, args.Principal.TenantID, id)
	if err != nil {
		return TestResult{}, err
	}
	if !ok {
		return TestResult{}, log.Code("connection_not_found").Errorf("there is no connection %s", id)
	}
	if err := CheckAccess(args, c, "use"); err != nil {
		return TestResult{}, err
	}
	contract, err := checkContract(args.Context(), d, args.Principal.TenantID, c.Type)
	if err != nil {
		return TestResult{OK: false, Error: connectors.AsError(err)}, nil
	}
	call := args
	call.Ctx = testing(args.Context())
	_, e := d.Dispatcher.Call(call, connectors.Call{Contract: contract, Operation: CheckOperation, Connection: id})
	if e != nil {
		return TestResult{Error: e}, nil
	}
	SetStatus(args.Context(), d, args.Principal.TenantID, id, StatusOK, "")
	return TestResult{OK: true}, nil
}

// checkContract is the contract of typ that declares the check operation.
func checkContract(ctx context.Context, d Deps, tenant uint64, typ string) (string, error) {
	t, ok, err := d.Registry.Type(ctx, tenant, typ)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", connectors.Errorf(connectors.CodeNotSupported, "there is no connector type %s", typ)
	}
	for _, ref := range t.Type.Contracts {
		if info, ok, _ := d.Registry.Contract(ctx, tenant, ref); ok {
			if _, has := info.Contract.Operation(CheckOperation); has {
				return ref, nil
			}
		}
	}
	return "", connectors.Errorf(connectors.CodeNotSupported, "%s declares no %s operation", typ, CheckOperation)
}
