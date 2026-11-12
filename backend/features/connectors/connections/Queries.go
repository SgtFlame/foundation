package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/nats-io/nats.go/jetstream"
)

// ContributeAccess declares the connection resource type: a user's own
// connection is theirs (Owned, fenced from tenant grants by their user
// segment), a tenant connection is the grants'.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "connection", Source: "connectors", Order: 96}, Title: "Connections", Parent: "tenant",
		Owned: true, Fence: "user",
		Actions: []authz.Action{
			{Name: "read", Title: "See connections: their type, settings without secrets and status", Class: authz.ClassRead},
			{Name: "use", Title: "Call a connection, and let a trigger run on its events", Class: authz.ClassExecute},
			{Name: "manage", Title: "Create, change and delete the tenant's connections", Class: authz.ClassAdmin},
		},
	})
	// A connector type's access group (forge 07f): tenant connections of
	// its types live under tenant:<t>/connectors:<group>, where a role
	// scoped to it reaches them and no others. It has no actions of its own.
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "connectors", Source: "connectors", Order: 97}, Title: "Connector groups", Parent: "tenant",
		Actions: []authz.Action{},
	})
}

// list reads every connection of the tenant.
func list(ctx context.Context, d Deps, tenant uint64) ([]Connection, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := d.Projections.Watch(ctx, tenant, Projector, TargetType+".*")
	if err != nil {
		return nil, err
	}
	var out []Connection
	for e := range entries {
		if e == nil {
			break
		}
		if e.Deleted {
			continue
		}
		var c Connection
		if json.Unmarshal(e.Value.Value, &c) == nil {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || (out[i].Name == out[j].Name && out[i].ID < out[j].ID)
	})
	return out, nil
}

// List is the connections the principal may read, of a type or of types
// implementing a contract when given.
func List(args features.ServiceArgs, d Deps, typ, contract string) ([]View, error) {
	all, err := list(args.Context(), d, args.Principal.TenantID)
	if err != nil {
		return nil, err
	}
	implements := map[string]bool{}
	out := []View{}
	for _, c := range all {
		if typ != "" && c.Type != typ {
			continue
		}
		if contract != "" {
			ok, seen := implements[c.Type]
			if !seen {
				if t, found, _ := d.Registry.Type(args.Context(), args.Principal.TenantID, c.Type); found {
					ok = slices.Contains(t.Type.Contracts, contract)
				}
				implements[c.Type] = ok
			}
			if !ok {
				continue
			}
		}
		if CheckAccess(args, c, "read") != nil {
			continue
		}
		out = append(out, ViewOf(c))
	}
	return out, nil
}

// rerun sends read's value, then again each time the connections change
// (and every few seconds, for what lives outside the projection), when it
// differs.
func rerun(f live.Feed, d Deps, every time.Duration, read func(ctx context.Context) (any, error)) error {
	ctx := f.Context()
	updates, err := d.Projections.Updates(ctx, f.Args().Principal.TenantID, Projector, TargetType+".*")
	if err != nil {
		return err
	}
	v, err := read(ctx)
	if err != nil {
		return err
	}
	last, _ := json.Marshal(v)
	f.Snapshot(json.RawMessage(last), live.Position{})
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-updates:
		case <-tick.C:
		}
		v, err := read(ctx)
		if err != nil {
			continue
		}
		raw, _ := json.Marshal(v)
		if bytes.Equal(raw, last) {
			continue
		}
		last = raw
		f.Change(live.Delta{Op: "replace", Value: raw}, live.Position{})
	}
}

// SourceView is an event source as the Jobs view lists it.
type SourceView struct {
	Connection string            `json:"connection"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Contracts  []string          `json:"contracts"`
	Online     bool              `json:"online"` // an adapter serves its type
	Holder     *connectors.Lease `json:"holder,omitempty"`
	Cursor     *time.Time        `json:"cursorAt,omitempty"`
	LastEvent  string            `json:"lastEvent,omitempty"`
}

// Sources are the tenant's event sources with who runs each.
func Sources(ctx context.Context, d Deps, tenant uint64) ([]SourceView, error) {
	js := d.Projections.JetStream()
	skv, err := connectors.SourcesKV(ctx, js)
	if err != nil {
		return nil, err
	}
	lkv, err := connectors.LeasesKV(ctx, js)
	if err != nil {
		return nil, err
	}
	ckv, err := connectors.CursorsKV(ctx, js)
	if err != nil {
		return nil, err
	}
	out := []SourceView{}
	keys, err := skv.ListKeysFiltered(ctx, connectors.SourceKey(tenant, "*"))
	if err != nil {
		return nil, err
	}
	for k := range keys.Keys() {
		e, err := skv.Get(ctx, k)
		if err != nil {
			continue
		}
		var src connectors.Source
		if json.Unmarshal(e.Value(), &src) != nil {
			continue
		}
		v := SourceView{Connection: src.Connection, Type: src.Type, Contracts: src.Contracts}
		if c, ok, _ := Get(ctx, d.Projections, tenant, src.Connection); ok {
			v.Name = c.Name
		}
		if t, ok, _ := d.Registry.Type(ctx, tenant, src.Type); ok {
			v.Online = t.Online
		}
		if le, err := lkv.Get(ctx, k); err == nil {
			var l connectors.Lease
			if json.Unmarshal(le.Value(), &l) == nil {
				v.Holder = &l
			}
		}
		if ce, err := ckv.Get(ctx, k); err == nil {
			var c connectors.Cursor
			if json.Unmarshal(ce.Value(), &c) == nil {
				v.Cursor, v.LastEvent = &c.At, c.LastEvent
			}
		} else if !errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, err
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "connections.list", Source: "connectors"}, Title: "Connections",
		Description: "The connections you may read: {connections: [{id, name, type, owner (tenant or user:<id>), settings, secrets {<field>: {set}}, status (ok, error, disabled), lastError, checkedAt, options {pollInterval, since, paused}}]}, of one connector type or of the types implementing a contract when given. Secrets are never shown.",
		Params: json.RawMessage(`{"type":"object","properties":{
  "type":{"type":"string","description":"Only connections of this connector type"},
  "contract":{"type":"string","description":"Only connections whose type implements this contract, id@version"}}}`),
		Examples: []json.RawMessage{json.RawMessage(`{"contract":"mail.mailbox@1"}`)},
		Action:   "connection.read",
		Run: func(f live.Feed) error {
			var p struct{ Type, Contract string }
			if len(f.Params()) > 0 {
				_ = json.Unmarshal(f.Params(), &p)
			}
			d := deps()
			return rerun(f, d, 5*time.Second, func(ctx context.Context) (any, error) {
				args := f.Args()
				args.Ctx = ctx
				views, err := List(args, d, p.Type, p.Contract)
				return map[string]any{"connections": views}, err
			})
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "connections.describe", Source: "connectors"}, Title: "Describe a connection",
		Description: "One connection: {connection: {id, name, type, owner, settings, secrets {<field>: {set}}, status, lastError, checkedAt, options}} (null once deleted), with its type's metadata as type. Secrets are never shown.",
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The connection's id"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"123"}`)},
		Action:      "connection.read",
		Run: func(f live.Feed) error {
			var p struct{ ID string }
			_ = json.Unmarshal(f.Params(), &p)
			d := deps()
			return rerun(f, d, 10*time.Second, func(ctx context.Context) (any, error) {
				args := f.Args()
				args.Ctx = ctx
				c, ok, err := Get(ctx, d.Projections, args.Principal.TenantID, p.ID)
				if err != nil {
					return nil, err
				}
				if !ok {
					return map[string]any{"connection": nil}, nil
				}
				if err := CheckAccess(args, c, "read"); err != nil {
					return nil, err
				}
				out := map[string]any{"connection": ViewOf(c)}
				if t, ok, _ := d.Registry.Type(ctx, args.Principal.TenantID, c.Type); ok {
					out["type"] = t
				}
				return out, nil
			})
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "connectors.sources", Source: "connectors"}, Title: "Event sources",
		Description: "The tenant's connector event sources (connections whose events an installed trigger listens to): {sources: [{connection, name, type, contracts, online (an adapter serves its type), holder {adapter, instance, host, since, lastError, lastErrorAt}, cursorAt, lastEvent}]}.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "job.manage",
		Run: func(f live.Feed) error {
			d := deps()
			return rerun(f, d, 2*time.Second, func(ctx context.Context) (any, error) {
				s, err := Sources(ctx, d, f.Args().Principal.TenantID)
				return map[string]any{"sources": s}, err
			})
		},
	})
	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: "connection.test", Source: "connectors"}, Title: "Test a connection",
		Description: "Signs in to a connection's service and does nothing (its contract's check operation), recording its status. Ends with one result event {ok, error {code, message}}.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var a struct{ ID string }
			if err := json.Unmarshal(c.Args, &a); err != nil || a.ID == "" {
				return c.Fail(http.StatusBadRequest, log.Code("invalid_args").Errorf("connection.test needs an id"))
			}
			res, err := Test(c.ServiceArgs, deps(), a.ID)
			if err != nil {
				return c.Fail(http.StatusForbidden, err)
			}
			c.Emit("result", res)
			return nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "connection.test", Source: "connectors"}, Title: "Test a connection", Effect: tools.EffectExternal,
		Description: "Signs in to a connection's service and does nothing, to see whether its settings work; records its status. Gives {ok, error {code, message}}.",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The connection's id (connections.list)"}},"required":["id"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				ID string `json:"id"`
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			res, err := Test(call.ServiceArgs, deps(), a.ID)
			if err != nil {
				return tools.Result{}, err
			}
			summary := "The connection works."
			if !res.OK {
				summary = "The connection failed: " + strings.TrimSpace(res.Error.Code+" "+res.Error.Message)
			}
			raw, _ := json.Marshal(res)
			var content map[string]any
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})
}
