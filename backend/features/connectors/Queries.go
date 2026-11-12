package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go"
)

// ContributeAccess declares the connector resource type: reading the
// registry (07b). Using a connection is 07c's.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "connector", Source: "connectors", Order: 95}, Title: "Connectors", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See the connector registry: contracts, connector types and their operations", Class: authz.ClassRead},
		},
	})
}

// Deps are what the connector queries and nodes need, made after the
// registry.
type Deps struct {
	Registry   *Registry
	Dispatcher *Dispatcher
}

// Contribute registers the registry's queries.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "connectors.contracts", Source: "connectors"}, Title: "Connector contracts",
		Description: "The tenant's connector contracts: {contracts: [{ref, source (builtin, announced), online, needsConnection, types, contract: {id, version, meta {title, description, tags}, operations [{id, title, description, args, result, examples, effect, destructive, key, errors, callbacks}], events [{id, title, description, payload, example, idFields, delivery, subscription}]}}]}. Changes when an adapter announces a type or goes offline.",
		Params:      json.RawMessage(`{"type":"object","properties":{"search":{"type":"string","description":"Words every contract listed holds in its id, title, description, tags or operations"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"search":"mail"}`)},
		Action:      "connector.read",
		Run: func(f live.Feed) error {
			var p struct{ Search string }
			_ = json.Unmarshal(orEmpty(f.Params()), &p)
			return follow(f, deps, func(ctx context.Context) (any, error) {
				cat, err := deps().Registry.Catalog(ctx, f.Args().Principal.TenantID)
				if err != nil {
					return nil, err
				}
				out := []ContractInfo{}
				for _, c := range cat.Contracts {
					if matches(p.Search, c.Ref, c.Contract) {
						out = append(out, c)
					}
				}
				return map[string]any{"contracts": out}, nil
			})
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "connectors.types", Source: "connectors"}, Title: "Connector types",
		Description: "The tenant's connector types and surfaces: {types: [{type: {id, contracts, meta, settings, noConnection, source, discovers, receives}, source, direction (outbound), online, instances}], surfaces: [{surface: {id, meta}, direction (inbound), online, instances}]}. A type is online while an adapter instance serving it announces it.",
		Params: json.RawMessage(`{"type":"object","properties":{
  "contract":{"type":"string","description":"Only the types implementing this contract (id@version)"},
  "search":{"type":"string","description":"Words every type listed holds in its id, title, description or tags"},
  "direction":{"type":"string","enum":["outbound","inbound"],"description":"outbound lists only types, inbound only surfaces"}}}`),
		Examples: []json.RawMessage{json.RawMessage(`{"contract":"mail.mailbox@1"}`)},
		Action:   "connector.read",
		Run: func(f live.Feed) error {
			var p struct{ Contract, Search, Direction string }
			_ = json.Unmarshal(orEmpty(f.Params()), &p)
			return follow(f, deps, func(ctx context.Context) (any, error) {
				cat, err := deps().Registry.Catalog(ctx, f.Args().Principal.TenantID)
				if err != nil {
					return nil, err
				}
				types, surfaces := []TypeInfo{}, []SurfaceEntry{}
				if p.Direction != Inbound {
					for _, t := range cat.Types {
						if p.Contract != "" && !implements(t.Type, p.Contract) {
							continue
						}
						if words(p.Search, t.Type.ID+" "+t.Type.Meta.Title+" "+t.Type.Meta.Description+" "+strings.Join(t.Type.Meta.Tags, " ")) {
							types = append(types, t)
						}
					}
				}
				if p.Direction != Outbound && p.Contract == "" {
					for _, s := range cat.Surfaces {
						if words(p.Search, s.Surface.ID+" "+s.Surface.Meta.Title+" "+s.Surface.Meta.Description+" "+strings.Join(s.Surface.Meta.Tags, " ")) {
							surfaces = append(surfaces, s)
						}
					}
				}
				return map[string]any{"types": types, "surfaces": surfaces}, nil
			})
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "connectors.describe", Source: "connectors"}, Title: "Describe a connector",
		Description: "One registry entry in full by ref: contract:<id[@version]> (its operations and events with their schemas and examples, and the types implementing it), connector:<type> (the type with its contracts), surface:<id>, or connection:<id> (a connection's discovered contract, with the operations left out as problems).",
		Params:      json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string","description":"contract:<id[@version]>, connector:<type>, surface:<id> or connection:<id>"}},"required":["ref"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"ref":"contract:mail.mailbox@1"}`)},
		Action:      "connector.read",
		Run: func(f live.Feed) error {
			var p struct{ Ref string }
			if err := json.Unmarshal(orEmpty(f.Params()), &p); err != nil || p.Ref == "" {
				return log.Code("bad_ref").Errorf("connectors.describe needs a ref")
			}
			return follow(f, deps, func(ctx context.Context) (any, error) {
				return deps().Registry.Describe(ctx, f.Args().Principal.TenantID, p.Ref)
			})
		},
	})
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// follow sends read's value, then the whole value again each time the
// registry or a discovered contract changes it.
func follow(f live.Feed, deps func() Deps, read func(ctx context.Context) (any, error)) error {
	r := deps().Registry
	if r == nil {
		return log.Errorf("there is no connector registry")
	}
	ctx := f.Context()
	changes, stop, err := r.Changes(ctx)
	if err != nil {
		return err
	}
	defer stop()
	discovered := make(chan struct{}, 1)
	if sub, err := r.js.Conn().Subscribe(fmt.Sprintf("cx.discovered.%d.*", f.Args().Principal.TenantID), func(*nats.Msg) {
		select {
		case discovered <- struct{}{}:
		default:
		}
	}); err == nil {
		defer func() { _ = sub.Unsubscribe() }()
	}
	v, err := read(ctx)
	if err != nil {
		return err
	}
	last, _ := json.Marshal(v)
	f.Snapshot(json.RawMessage(last), live.Position{})
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-changes:
		case <-discovered:
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

// matches reports whether a contract holds every word of a search.
func matches(search, ref string, c Contract) bool {
	hay := ref + " " + c.Meta.Title + " " + c.Meta.Description + " " + strings.Join(c.Meta.Tags, " ")
	for _, op := range c.Operations {
		hay += " " + op.ID + " " + op.Title + " " + op.Description
	}
	for _, e := range c.Events {
		hay += " " + e.ID + " " + e.Title + " " + e.Description
	}
	return words(search, hay)
}

func words(search, hay string) bool {
	hay = strings.ToLower(hay)
	for _, w := range strings.Fields(strings.ToLower(search)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}
