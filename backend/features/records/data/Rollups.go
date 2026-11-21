package data

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// Rollups is the follower that keeps rollups current (Records.md › Built
// in slice 24): for every record event, it finds the records whose
// rollups read the changed record, through the relation fields that point
// to it, and sends each record.recompute. A recompute that changes nothing
// commits nothing, so rollups over rollups settle.
const Rollups = "records.rollups"

// published caches each tenant's published entity types, for the rollups
// follower; a publication clears it.
var published sync.Map // tenant → []records.EntityType

// forget drops a tenant's cached published types.
func forget(tenant uint64) { published.Delete(tenant) }

// PublishedTypes are the tenant's published entity types.
func PublishedTypes(ctx context.Context, deps Deps, tenant uint64) ([]records.EntityType, error) {
	if v, ok := published.Load(tenant); ok {
		return v.([]records.EntityType), nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := deps.Projections.Watch(ctx, tenant, entities.PublishedProjector, entities.TargetType+".*")
	if err != nil {
		return nil, err
	}
	out := []records.EntityType{}
	for en := range entries {
		if en == nil {
			break
		}
		var e records.EntityType
		if !en.Deleted && json.Unmarshal(en.Value.Value, &e) == nil && !e.Hidden {
			out = append(out, e)
		}
	}
	published.Store(tenant, out)
	return out, nil
}

// followRollups starts the rollups follower in the principal's tenant.
func followRollups(args features.ServiceArgs, deps Deps) error {
	tenant := args.Principal.TenantID
	subject := fmt.Sprintf("evt.%d.%s.*.*.*", tenant, live.AreaRecords)
	return deps.Projections.Follow(args.Context(), tenant, live.AreaRecords, Rollups, []string{subject}, func(e live.Event) error {
		return recomputeRollups(deps, e)
	})
}

func recomputeRollups(deps Deps, e live.Event) error {
	if e.Target.Type == CounterType {
		return nil
	}
	ctx := context.Background()
	types, err := PublishedTypes(ctx, deps, e.TenantID)
	if err != nil {
		return err
	}
	refs := rollupsOver(types, e.Target.Type)
	if len(refs) == 0 {
		return nil
	}
	// The recompute reads the changed record from the projection
	if err := WaitFolded(ctx, deps, e.TenantID, e.Seq); err != nil {
		return err
	}
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(e.TenantID)}
	for _, ref := range refs {
		op := "eq"
		if ref.relation.Relation.Many {
			op = "contains"
		}
		res, err := Find(args, deps, Query{Entity: ref.entity, Filter: &Filter{Field: ref.relation.ID, Op: op, Value: raw(e.Target.ID)}, Fields: []string{ref.relation.ID}})
		if err != nil {
			return err
		}
		for _, r := range res.Records {
			var id string
			_ = json.Unmarshal(r[MetaID], &id)
			a, _ := json.Marshal(map[string]any{"entity": ref.entity})
			resp := deps.Bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("rollup-%d-%s-%s", e.Seq, ref.entity, id), Type: "record.recompute", Target: id, Args: a})
			if resp.Common.Code >= 300 && resp.Common.Code != 422 {
				return fmt.Errorf("cannot recompute %s %s: %s", ref.entity, id, resp.Common.Message)
			}
		}
	}
	return nil
}

// WaitFolded waits until the records projector has folded the event at
// seq.
func WaitFolded(ctx context.Context, deps Deps, tenant, seq uint64) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stream := live.StreamName(tenant, live.AreaRecords)
	for {
		pos, err := deps.Projections.Folded(ctx, tenant, Projector)
		if err != nil {
			return err
		}
		if pos[stream] >= seq {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// recomputeAll sends record.recompute for every record of entity, after
// publication seq changed its computed fields.
func recomputeAll(deps Deps, tenant uint64, entity string, seq uint64) error {
	todo, err := RecordsOf(deps, tenant, entity)
	if err != nil {
		return err
	}
	args := features.ServiceArgs{Ctx: context.Background(), Principal: features.ServicePrincipal(tenant)}
	a, _ := json.Marshal(map[string]any{"entity": entity})
	for _, r := range todo {
		var id string
		_ = json.Unmarshal(r[MetaID], &id)
		resp := deps.Bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("recompute-%d-%s", seq, id), Type: "record.recompute", Target: id, Args: a})
		if resp.Common.Code >= 300 && resp.Common.Code != 422 {
			return fmt.Errorf("cannot recompute %s %s: %s", entity, id, resp.Common.Message)
		}
	}
	return nil
}

// RecordsOf is every record of an entity type, as projected.
func RecordsOf(deps Deps, tenant uint64, entity string) ([]Record, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entries, err := deps.Projections.Watch(ctx, tenant, Projector, entity+".*")
	if err != nil {
		return nil, err
	}
	var out []Record
	for en := range entries {
		if en == nil {
			break
		}
		var r Record
		if !en.Deleted && json.Unmarshal(en.Value.Value, &r) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}
