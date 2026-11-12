package connections

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	connectorsnodes "github.com/SgtFlame/foundation/backend/features/connectors/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/nats-io/nats.go/jetstream"
)

// Event sources, Foundation's side (forge 07c): cx_sources lists each
// connection on which an installed trigger listens (On connector event
// with its contract, unfiltered or filtered to it) and that is neither
// disabled, errored, paused nor unusable by the trigger's app. Every
// process computes the same list from the projections and writes only
// what differs, on each connection or install event and every minute, so
// no process leads and any may do it.

// listener is an installed trigger's On connector event.
type listener struct {
	app        string
	contract   string // id@version
	connection string // "" for every one
}

// listeners are the tenant's installed triggers on connector events.
func listeners(ctx context.Context, d Deps, tenant uint64) ([]listener, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := d.Projections.Watch(ctx, tenant, installs.Projector, installs.TargetType+".*")
	if err != nil {
		return nil, err
	}
	var out []listener
	for e := range entries {
		if e == nil {
			break
		}
		var in apps.Install
		if e.Deleted || json.Unmarshal(e.Value.Value, &in) != nil || !in.Installed() {
			continue
		}
		for _, tr := range in.Version.Manifest.Triggers {
			_, g, ok := in.Version.Trigger(tr.ID)
			if !ok {
				continue
			}
			if l, ok := listenerOf(in.App, g); ok {
				out = append(out, l)
			}
		}
	}
	return out, nil
}

func listenerOf(app string, g cognition.Graph) (listener, bool) {
	for _, n := range g.Nodes {
		if n.Type != connectorsnodes.On {
			continue
		}
		var c connectorsnodes.OnConfig
		if json.Unmarshal(n.Config, &c) != nil || c.Contract == "" {
			return listener{}, false
		}
		return listener{app: app, contract: c.Contract, connection: c.Connection}, true
	}
	return listener{}, false
}

// revision is what restarts a running source when it changes: the
// connection's settings, secrets and options.
func revision(c Connection) uint64 {
	raw, _ := json.Marshal([]any{c.Type, c.Settings, c.Secrets, c.Options})
	sum := sha256.Sum256(raw)
	return binary.BigEndian.Uint64(sum[:8])
}

// Reconcile makes the tenant's cx_sources what its installs and
// connections say.
func Reconcile(ctx context.Context, d Deps, tenant uint64) error {
	ls, err := listeners(ctx, d, tenant)
	if err != nil {
		return err
	}
	conns, err := list(ctx, d, tenant)
	if err != nil {
		return err
	}
	want := map[string]connectors.Source{}
	for _, c := range conns {
		if c.Status != StatusOK || c.Options.Paused {
			continue
		}
		t, ok, err := d.Registry.Type(ctx, tenant, c.Type)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		var contracts []string
		for _, l := range ls {
			if !slices.Contains(t.Type.Contracts, l.contract) || (l.connection != "" && l.connection != c.ID) || slices.Contains(contracts, l.contract) {
				continue
			}
			as := features.ServiceArgs{Ctx: ctx, Principal: features.AppPrincipal(tenant, l.app)}
			if CheckAccess(as, c, "use") != nil {
				continue
			}
			contracts = append(contracts, l.contract)
		}
		if len(contracts) == 0 {
			continue
		}
		slices.Sort(contracts)
		want[connectors.SourceKey(tenant, c.ID)] = connectors.Source{Tenant: tenant, Connection: c.ID, Type: c.Type, Contracts: contracts,
			PollInterval: c.Options.PollInterval, Since: c.Options.Since, Revision: revision(c)}
	}
	kv, err := connectors.SourcesKV(ctx, d.Projections.JetStream())
	if err != nil {
		return err
	}
	have := map[string]connectors.Source{}
	keys, err := kv.ListKeysFiltered(ctx, connectors.SourceKey(tenant, "*"))
	if err != nil {
		return err
	}
	for k := range keys.Keys() {
		if e, err := kv.Get(ctx, k); err == nil {
			var s connectors.Source
			if json.Unmarshal(e.Value(), &s) == nil {
				have[k] = s
			}
		}
	}
	for k := range have {
		if _, ok := want[k]; !ok {
			if err := kv.Delete(ctx, k); err != nil {
				return err
			}
		}
	}
	for k, s := range want {
		if old, ok := have[k]; ok {
			s.At = old.At
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(s)
			if bytes.Equal(a, b) {
				continue
			}
		}
		s.At = time.Now().UTC()
		raw, _ := json.Marshal(s)
		if _, err := kv.Put(ctx, k, raw); err != nil {
			return err
		}
	}
	return nil
}

// Follower is the follower that keeps cx_sources and purges deleted
// connections' secrets.
const Follower = "connectors.sources"

// Start keeps each tenant's sources: a follower on connection and install
// events (one durable consumer shared by every process) and a pass every
// minute, until ctx ends. tenants lists the tenants to serve.
func Start(ctx context.Context, d func() Deps, tenants func() ([]uint64, error)) {
	served := map[uint64]bool{}
	var mu sync.Mutex
	serve := func(tenant uint64) {
		mu.Lock()
		defer mu.Unlock()
		if served[tenant] {
			return
		}
		subjects := []string{
			live.Target{Area: live.AreaDefinitions, Type: TargetType, ID: "*"}.Subjects(tenant),
			live.Target{Area: live.AreaDefinitions, Type: installs.TargetType, ID: "*"}.Subjects(tenant),
		}
		err := d().Projections.Follow(ctx, tenant, live.AreaDefinitions, Follower, subjects, func(e live.Event) error {
			return followed(ctx, d(), tenant, e)
		})
		if err != nil {
			log.Error(ctx, err, "connections: cannot follow the connections of tenant %d", tenant)
			return
		}
		served[tenant] = true
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		ids, err := tenants()
		if err != nil && ctx.Err() == nil {
			log.Warn(ctx, "connections: cannot list the tenants: %v", err)
		}
		for _, t := range ids {
			serve(t)
			if err := Reconcile(ctx, d(), t); err != nil && ctx.Err() == nil {
				log.Warn(ctx, "connections: cannot keep the event sources of tenant %d: %v", t, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// followed handles one connection or install event: once the projection
// has it, the sources are made again; a deleted connection's secrets are
// forgotten.
func followed(ctx context.Context, d Deps, tenant uint64, e live.Event) error {
	projector := Projector
	if e.Target.Type == installs.TargetType {
		projector = installs.Projector
	}
	if _, err := d.Projections.WaitFor(ctx, tenant, projector, e.Target.Key(), e.Seq, 10*time.Second); err != nil {
		return err
	}
	if e.Target.Type == TargetType && e.Type == EventDeleted {
		if err := purgeSecrets(ctx, d, tenant, e.Target.ID); err != nil {
			return err
		}
	}
	if e.Target.Type == installs.TargetType {
		if err := disableRemoved(ctx, d, tenant, e); err != nil {
			return err
		}
	}
	return Reconcile(ctx, d, tenant)
}

// disableRemoved disables the connections of scripted types an install
// event took away (forge 07d): all of an uninstalled app's, and those an
// upgrade's version no longer declares. Enabling one again is the user's
// choice once the type is back.
func disableRemoved(ctx context.Context, d Deps, tenant uint64, e live.Event) error {
	var removed []string
	in, ok := installs.Get(ctx, d.Projections, tenant, e.Target.ID)
	if !ok {
		return nil
	}
	switch e.Type {
	case installs.EventUninstalled:
		for _, k := range in.Version.Manifest.Connectors {
			removed = append(removed, k.TypeID(in.App))
		}
	case installs.EventUpgraded:
		var c struct {
			From int `json:"from"`
		}
		_ = json.Unmarshal(e.Data, &c)
		args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
		old, ok := manifests.GetVersion(args, d.Projections, in.App, c.From)
		if !ok {
			return nil
		}
		for _, k := range old.Manifest.Connectors {
			if _, still := in.Version.Connector(k.Name); !still {
				removed = append(removed, k.TypeID(in.App))
			}
		}
	}
	if len(removed) == 0 {
		return nil
	}
	conns, err := list(ctx, d, tenant)
	if err != nil {
		return err
	}
	for _, c := range conns {
		if !slices.Contains(removed, c.Type) || c.Status == StatusDisabled {
			continue
		}
		args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
		res := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("removed-%d-%s", e.Seq, c.ID), Type: "connection.disable", Target: c.ID, Args: json.RawMessage(`{}`)})
		if res.Common.Code >= 300 {
			log.With("connection", c.ID).Warn(ctx, "connection %s of the removed type %s could not be disabled: %s", c.ID, c.Type, res.Common.Message)
		}
	}
	return nil
}

// ContributeTrigger registers On connector event as a trigger event: a
// trigger's durable consumer on CX_<tenant>, filtered by its contract,
// event and connection. A trigger filtered to a connection its app may
// not use is refused (it does not run, and says why); an unfiltered one
// skips the events of connections its app may not use.
func ContributeTrigger(reg *extensions.Registry, deps func() Deps) {
	livenodes.TriggerEvents.Register(reg, livenodes.TriggerEvent{
		Descriptor: extensions.Descriptor{ID: connectorsnodes.On, Source: "connectors"},
		Subjects: func(args features.ServiceArgs, _ string, config json.RawMessage) (live.Area, []string, error) {
			var c connectorsnodes.OnConfig
			if err := json.Unmarshal(config, &c); err != nil {
				return "", nil, fmt.Errorf("On connector event's config cannot be read: %v", err)
			}
			id, version, err := connectors.ParseRef(c.Contract)
			if err != nil || version == 0 {
				return "", nil, fmt.Errorf("On connector event needs a contract id@version, not %q", c.Contract)
			}
			if !live.ValidToken(connectors.Token(c.Event)) || c.Event == "" {
				return "", nil, fmt.Errorf("On connector event needs an event, not %q", c.Event)
			}
			if c.Connection != "" {
				conn, ok, err := Get(args.Context(), deps().Projections, args.Principal.TenantID, c.Connection)
				if err != nil {
					return "", nil, err
				}
				if !ok {
					return "", nil, fmt.Errorf("there is no connection %s", c.Connection)
				}
				if CheckAccess(args, conn, "use") != nil {
					return "", nil, fmt.Errorf("the app may not use connection %s, so a trigger cannot start on its events", conn.Name)
				}
			}
			return connectors.EventArea, []string{connectors.EventSubjectsOf(args.Principal.TenantID, c.Connection, id, c.Event)}, nil
		},
		Stream: func(ctx context.Context, js jetstream.JetStream, tenant uint64) (jetstream.Stream, error) {
			return connectors.EventStream(ctx, js, tenant)
		},
		Accept: func(args features.ServiceArgs, _ json.RawMessage, e live.Event) bool {
			c, ok, err := Get(args.Context(), deps().Projections, args.Principal.TenantID, e.Target.ID)
			return err == nil && ok && CheckAccess(args, c, "use") == nil
		},
		Inputs: func(_ features.ServiceArgs, config json.RawMessage, e live.Event) (cognition.Values, error) {
			var payload map[string]any
			_ = json.Unmarshal(e.Data, &payload)
			var c connectorsnodes.OnConfig
			_ = json.Unmarshal(config, &c)
			out := cognition.Values{}
			for k, v := range payload {
				out[k] = v
			}
			out["connection"] = e.Target.ID
			out["event"] = map[string]any{"contract": c.Contract, "name": e.Type, "id": e.CommandID, "payload": payload, "at": e.Time}
			return out, nil
		},
	})
}
