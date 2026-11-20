package data

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// Agents is authz's directory of agents (forge 07j): the records of entity
// types with a principal field. An agent is its record: its id the
// record's, its name the record's title, its creator the record's, and it
// is retired once the record is deleted. Nothing is minted or copied, so
// every process answers the same from the projections.
type Agents struct {
	Deps func() Deps
}

var _ authz.Agents = Agents{}

// principalTypes are the tenant's published entity types with a principal
// field.
func (a Agents) principalTypes(ctx context.Context, tenant uint64) ([]records.EntityType, error) {
	d := a.Deps()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := d.Projections.Watch(ctx, tenant, entities.PublishedProjector, entities.TargetType+".*")
	if err != nil {
		return nil, err
	}
	var out []records.EntityType
	for e := range entries {
		if e == nil {
			break
		}
		if e.Deleted {
			continue
		}
		var t records.EntityType
		if json.Unmarshal(e.Value.Value, &t) != nil || t.Hidden {
			continue
		}
		if _, ok := records.PrincipalField(t); ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// info is the agent a record defines; false when it defines none.
func info(tenant uint64, t records.EntityType, r Record) (authz.AgentInfo, bool) {
	f, _ := records.PrincipalField(t)
	var value string
	if json.Unmarshal(r[f.ID], &value) != nil {
		return authz.AgentInfo{}, false
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return authz.AgentInfo{}, false
	}
	var by uint64
	_ = json.Unmarshal(r["_createdBy"], &by)
	name := records.TitleOf(t, r)
	if name == "" {
		name = "Agent " + value
	}
	p := features.ServicePrincipal(tenant)
	app, _ := ownerApp(t.Owner)
	return authz.AgentInfo{ID: id, TenantID: tenant, Name: name, Entity: t.ID, App: app, CreatedBy: by,
		Resource: authz.RecordResource(records.Resource(p, t.ID), value)}, true
}

func ownerApp(owner string) (string, bool) {
	if len(owner) > 4 && owner[:4] == "app:" {
		return owner[4:], true
	}
	return "", false
}

// Agent is a live agent of the tenant.
func (a Agents) Agent(ctx context.Context, tenant, id uint64) (authz.AgentInfo, bool, error) {
	types, err := a.principalTypes(ctx, tenant)
	if err != nil {
		return authz.AgentInfo{}, false, err
	}
	d := a.Deps()
	key := strconv.FormatUint(id, 10)
	for _, t := range types {
		v, ok, err := d.Projections.Get(ctx, tenant, Projector, Key(t.ID, key))
		if err != nil {
			return authz.AgentInfo{}, false, err
		}
		if !ok || v.Value == nil {
			continue
		}
		var r Record
		if json.Unmarshal(v.Value, &r) != nil {
			continue
		}
		if ag, ok := info(tenant, t, r); ok && ag.ID == id {
			return ag, true, nil
		}
	}
	return authz.AgentInfo{}, false, nil
}

// List is the tenant's live agents.
func (a Agents) List(ctx context.Context, tenant uint64) ([]authz.AgentInfo, error) {
	types, err := a.principalTypes(ctx, tenant)
	if err != nil {
		return nil, err
	}
	d := a.Deps()
	var out []authz.AgentInfo
	for _, t := range types {
		err := func() error {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			entries, err := d.Projections.Watch(ctx, tenant, Projector, t.ID+".*")
			if err != nil {
				return err
			}
			for e := range entries {
				if e == nil {
					break
				}
				var r Record
				if e.Deleted || json.Unmarshal(e.Value.Value, &r) != nil {
					continue
				}
				if ag, ok := info(tenant, t, r); ok {
					out = append(out, ag)
				}
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
