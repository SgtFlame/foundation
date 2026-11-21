package scripted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var timeNow = func() time.Time { return time.Now().UTC() }

// Deps are what the scripted adapter and Emit connector event need, made
// after the registry.
type Deps struct {
	Store  live.Store // the installs and connections
	Engine *engine.Engine
	JS     jetstream.JetStream // CX_<tenant> and the cursors
	Reg    *extensions.Registry
}

// Options tune the scripted adapter.
type Options struct {
	// Every is how often the tenants are listed again (a minute).
	Every time.Duration
	// Adapter is passed on to each tenant's adapter.Serve (Name, timings);
	// its Types, Contracts, Impls and Tenants are the scripted adapter's.
	Adapter adapter.Options
}

// Serve runs the scripted adapter on nc until ctx ends: for each tenant
// tenants lists, an adapter.Serve over its installed scripted types,
// started again (the new one first, then the old one stopped) when what
// the installs declare changes. Every process runs one, so the types are
// served in their queue groups wherever an instance runs.
func Serve(ctx context.Context, nc *nats.Conn, deps func() Deps, tenants func() ([]uint64, error), o Options) {
	if o.Every <= 0 {
		o.Every = time.Minute
	}
	if o.Adapter.Name == "" {
		o.Adapter.Name = "scripted"
	}
	s := &server{nc: nc, deps: deps, o: o, tenants: map[uint64]*tenant{}}
	tick := time.NewTicker(o.Every)
	defer tick.Stop()
	for {
		ids, err := tenants()
		if err != nil && ctx.Err() == nil {
			log.Warn(ctx, "scripted connectors: cannot list the tenants: %v", err)
		}
		for _, id := range ids {
			s.serve(ctx, id)
		}
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return
		case <-tick.C:
		}
	}
}

type server struct {
	nc      *nats.Conn
	deps    func() Deps
	o       Options
	mu      sync.Mutex
	tenants map[uint64]*tenant
	wg      sync.WaitGroup
}

// tenant is one tenant's running adapter.
type tenant struct {
	id     uint64
	mu     sync.Mutex
	print  string
	cancel context.CancelFunc
}

// serve starts following a tenant's installs, once.
func (s *server) serve(ctx context.Context, id uint64) {
	s.mu.Lock()
	if s.tenants[id] != nil {
		s.mu.Unlock()
		return
	}
	t := &tenant{id: id}
	s.tenants[id] = t
	s.mu.Unlock()
	entries, err := s.deps().Store.Watch(ctx, id, installs.Projector, installs.TargetType+".*")
	if err != nil {
		log.Warn(ctx, "scripted connectors: cannot follow the installs of tenant %d: %v", id, err)
		s.mu.Lock()
		delete(s.tenants, id)
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		all := map[string]apps.Install{}
		started := false
		for e := range entries {
			if e == nil {
				started = true
				s.apply(ctx, t, all)
				continue
			}
			var in apps.Install
			if e.Deleted || json.Unmarshal(e.Value.Value, &in) != nil {
				delete(all, e.Key)
			} else {
				all[e.Key] = in
			}
			if started {
				s.apply(ctx, t, all)
			}
		}
		t.stop()
	}()
}

// Types are the scripted types the installs declare, with the contracts
// they name, sorted by id.
func Types(all map[string]apps.Install, contract func(string) (connectors.Contract, bool)) ([]connectors.Type, []connectors.Contract) {
	var types []connectors.Type
	var contracts []connectors.Contract
	for _, in := range all {
		if !in.Installed() {
			continue
		}
		for _, k := range in.Version.Manifest.Connectors {
			t := apps.ConnectorType(in.App, k)
			ok := true
			for _, ref := range t.Contracts {
				c, found := contract(ref)
				if !found {
					ok = false
					break
				}
				if !slices.ContainsFunc(contracts, func(x connectors.Contract) bool { return x.Ref() == ref }) {
					contracts = append(contracts, c)
				}
			}
			if ok && len(connectors.CheckType(t, contract)) == 0 {
				types = append(types, t)
			}
		}
	}
	slices.SortFunc(types, func(a, b connectors.Type) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(contracts, func(a, b connectors.Contract) int { return strings.Compare(a.Ref(), b.Ref()) })
	return types, contracts
}

// apply serves what the installs declare now, if it changed.
func (s *server) apply(ctx context.Context, t *tenant, all map[string]apps.Install) {
	d := s.deps()
	types, contracts := Types(all, apps.BuiltinContracts(d.Reg))
	raw, _ := json.Marshal(types)
	t.mu.Lock()
	defer t.mu.Unlock()
	if string(raw) == t.print {
		return
	}
	t.print = string(raw)
	old := t.cancel
	t.cancel = nil
	if len(types) > 0 {
		o := s.o.Adapter
		o.Types, o.Contracts, o.Tenants = types, contracts, []uint64{t.id}
		o.Impls = nil
		for _, ty := range types {
			app, name, _ := strings.Cut(ty.ID, ".")
			o.Impls = append(o.Impls, adapter.Impl{Type: ty.ID, Open: s.opener(app, name)})
		}
		actx, cancel := context.WithCancel(ctx)
		t.cancel = cancel
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := adapter.Serve(actx, s.nc, o); err != nil && actx.Err() == nil {
				log.Error(ctx, err, "scripted connectors of tenant %d cannot be served", t.id)
			}
		}()
	}
	if old != nil {
		// The new instance subscribes first, so no call finds no responder
		time.AfterFunc(time.Second, old)
	}
}

func (t *tenant) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
}

// opener opens sessions of one scripted type.
func (s *server) opener(app, name string) func(adapter.Context, adapter.Connection) (adapter.Session, error) {
	return func(_ adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var settings map[string]any
		if len(conn.Settings) > 0 {
			_ = json.Unmarshal(conn.Settings, &settings)
		}
		if settings == nil {
			settings = map[string]any{}
		}
		return &session{s: s, app: app, name: name, conn: conn, settings: settings}, nil
	}
}

// session is a scripted type's session on a connection: each call reads
// the install again, so an upgrade's graphs answer the next call.
type session struct {
	s        *server
	app      string
	name     string
	conn     adapter.Connection
	settings map[string]any
}

func (*session) Close() error { return nil }

// version is the installed version declaring the session's type.
func (x *session) version(ctx adapter.Context) (apps.Version, error) {
	in, ok := installs.Running(ctx, x.s.deps().Store, ctx.Tenant(), x.app)
	if !ok {
		return apps.Version{}, connectors.Errorf(connectors.CodeNotSupported, "the app %s is not installed", x.app).NotRetryable()
	}
	if _, ok := in.Version.Connector(x.name); !ok {
		return apps.Version{}, connectors.Errorf(connectors.CodeNotSupported, "the app %s has no connector %s since version %d", x.app, x.name, in.Version.Version).NotRetryable()
	}
	return in.Version, nil
}

// run runs a pinned connector.op graph as the app's own principal.
func (x *session) run(ctx adapter.Context, g cognition.Graph, inputs cognition.Values, host *Host) (cognition.Values, error) {
	d := x.s.deps()
	if d.Engine == nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "graphs cannot run here")
	}
	args := features.ServiceArgs{Ctx: ctx.Context, Principal: features.AppPrincipal(ctx.Tenant(), x.app), Meta: ctx.Args.Meta.WithFeature("connector")}
	inputs["settings"] = x.settings
	inputs["connection"] = x.conn.ID
	res, err := d.Engine.Run(engine.RunArgs{ServiceArgs: args, GraphID: g.ID, Graph: &g, Event: apps.NodeOperation, Inputs: inputs,
		Trigger: "connector:" + apps.Scoped(x.app, x.name), Host: host, Trace: engine.TraceFailures})
	if err != nil {
		if e := (*connectors.Error)(nil); asError(err, &e) {
			return nil, e
		}
		ctx.Log.Warn(ctx, "%s.%s %s: run %d failed: %v", x.app, x.name, ctx.Operation, res.RunID, err)
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the connector %s.%s failed (run %d): %v", x.app, x.name, res.RunID, err)
	}
	if raw, ok := res.Outputs[ErrorKey]; ok {
		b, _ := json.Marshal(raw)
		var e connectors.Error
		if json.Unmarshal(b, &e) != nil || !connectors.ValidCode(e.Code) {
			return nil, connectors.Errorf(connectors.CodeUnavailable, "the connector %s.%s returned an error that cannot be read", x.app, x.name)
		}
		return nil, &e
	}
	return res.Outputs, nil
}

// Call answers one operation with its pinned graph; one the connector
// leaves out is notSupported.
func (x *session) Call(ctx adapter.Context, op string, raw json.RawMessage) (json.RawMessage, error) {
	v, err := x.version(ctx)
	if err != nil {
		return nil, err
	}
	g, ok := v.ConnectorGraph(x.name, op)
	if !ok {
		return nil, connectors.Errorf(connectors.CodeNotSupported, "%s.%s does not implement %s", x.app, x.name, op).NotRetryable()
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the args cannot be read: %v", err)
	}
	inputs := cognition.Values{}
	for k, val := range args {
		if !slices.Contains(reserved, k) {
			inputs[k] = val
		}
	}
	inputs["args"] = args
	inputs["caller"] = callerOf(ctx.Args.Principal)
	out, err := x.run(ctx, g, inputs, &Host{})
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for k, val := range out {
		if val != nil {
			result[k] = val
		}
	}
	return json.Marshal(result)
}

// Watch runs the type's watch graph once per pass, publishing the events
// it emitted (the new cursor with the last) and storing a new cursor when
// it emitted none. A type with no watch graph raises its events from the
// app's triggers, so its source only waits.
func (x *session) Watch(ctx adapter.Context, cursor json.RawMessage, emitFn func(adapter.Event) error) error {
	v, err := x.version(ctx)
	if err != nil {
		return err
	}
	g, ok := v.WatchGraph(x.name)
	if !ok {
		<-ctx.Done()
		return nil
	}
	var cur any
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &cur)
	}
	host := &Host{Watch: true}
	out, err := x.run(ctx, g, cognition.Values{"cursor": cur}, host)
	if err != nil {
		return err
	}
	var next json.RawMessage
	if c, ok := out["cursor"]; ok && c != nil {
		next, _ = json.Marshal(c)
	}
	for i, e := range host.Events {
		if i == len(host.Events)-1 {
			e.Cursor = next
		}
		if err := emitFn(e); err != nil {
			return err
		}
	}
	if len(host.Events) == 0 && next != nil && !bytes.Equal(next, cursor) {
		d := x.s.deps()
		kv, err := connectors.CursorsKV(ctx, d.JS)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(connectors.Cursor{Cursor: next, At: timeNow()})
		_, err = kv.Put(ctx, connectors.SourceKey(ctx.Tenant(), x.conn.ID), body)
		return err
	}
	return nil
}

// callerOf is the caller's principal as the entry's caller output.
func callerOf(p features.Principal) map[string]any {
	out := map[string]any{"kind": p.Kind, "tenant": fmt.Sprint(p.TenantID)}
	if p.UserID != 0 {
		out["user"] = fmt.Sprint(p.UserID)
	}
	if p.App != "" {
		out["app"] = p.App
	}
	return out
}

func asError(err error, target **connectors.Error) bool { return errors.As(err, target) }
