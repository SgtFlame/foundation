package installs

import (
	"context"
	"encoding/json"
	"maps"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// SeedFollower creates an installed version's sample records (forge
// iteration question 3).
const SeedFollower = "apps.seed"

// followSeed makes sure the principal's tenant gets its apps' sample
// records: each install and upgrade creates the version's seed records,
// as the tenant's service principal, in each of the app's own entity
// types that has no records but them, keeping their ids so relations
// between them hold. A record refused is logged and left out.
func followSeed(args features.ServiceArgs, deps Deps) error {
	if deps.Nodes.Bus == nil || deps.Nodes.Subscriptions == nil {
		return nil
	}
	tenant := args.Principal.TenantID
	t := live.Target{Area: live.AreaDefinitions, Type: TargetType, ID: "*"}
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, SeedFollower, []string{t.Subjects(tenant)}, func(e live.Event) error {
		if e.Type != EventInstalled && e.Type != EventUpgraded {
			return nil
		}
		var c change
		if err := json.Unmarshal(e.Data, &c); err != nil || c.Version == nil {
			return nil
		}
		as := features.ServiceArgs{Ctx: context.Background(), Principal: features.ServicePrincipal(e.TenantID)}
		for _, entity := range slices.Sorted(maps.Keys(c.Version.Seed)) {
			if slices.Contains(c.Version.Owned, entity) {
				seedEntity(as, deps, e.Target.ID, entity, c.Version.Seed[entity])
			}
		}
		return nil
	})
}

func seedEntity(args features.ServiceArgs, deps Deps, app, entity string, recs []apps.SeedRecord) {
	params, _ := json.Marshal(map[string]any{"entity": entity})
	raw, _, fail := deps.Nodes.Subscriptions.Once(args, "records.list", params)
	if fail != nil {
		log.Error(args.Context(), log.Errorf("%s", fail.Message), "cannot read the records of %s to seed them", entity)
		return
	}
	b, _ := json.Marshal(raw)
	var byKey map[string]data.Record
	_ = json.Unmarshal(b, &byKey)
	seeded := map[string]bool{}
	for _, r := range recs {
		seeded[r.ID] = true
	}
	have := map[string]bool{}
	for _, r := range byKey {
		var id string
		_ = json.Unmarshal(r[data.MetaID], &id)
		if !seeded[id] {
			return // the type has records of its own: it is not seeded
		}
		have[id] = true
	}
	for _, r := range recs {
		if have[r.ID] {
			continue
		}
		a, _ := json.Marshal(map[string]any{"entity": entity, "values": r.Values})
		res := deps.Nodes.Bus.Execute(bus.Request{ServiceArgs: args, ID: "seed-" + app + "-" + entity + "-" + r.ID, Type: "record.create", Target: r.ID, Args: a})
		if res.Common.Code >= 300 {
			log.Error(args.Context(), log.Code("app_seed_refused").With("appId", app).With("entity", entity).With("recordId", r.ID).Errorf("%s %+v", res.Common.Message, res.Problems), "a sample record was refused")
		}
	}
}
