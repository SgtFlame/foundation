package infraapply

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	infranodes "github.com/SgtFlame/foundation/backend/features/infra/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// RecordsActual is infra.Deps.Actual over the Control Plane's desired-state
// records: an instance exists when its infraInstance record does (by the
// record id its resource id gives), with the properties the configuration
// gives it, so a changed profile is not yet seen as a change (Tony to
// confirm); an infraInstance record no resource names is listed under its
// record id, for removal. A cluster exists once one of its hosts' records
// does (clusters are made in 22h).
func RecordsActual(reg *extensions.Registry, nd func() infranodes.Deps) func(args features.ServiceArgs, config string, docs []infra.Document) ([]infra.Resource, error) {
	return func(args features.ServiceArgs, config string, docs []infra.Document) ([]infra.Resource, error) {
		d := nd()
		if d.Data.Projections == nil {
			return nil, nil
		}
		byRecord := map[string]infra.Resource{}
		var clusters []infra.Resource
		for _, doc := range docs {
			rs, _ := infra.Expand(reg, doc)
			for _, r := range rs {
				byRecord[infranodes.RecordID(r.ID)] = r
				if r.Kind == infra.KindCluster {
					clusters = append(clusters, r)
				}
			}
		}
		_ = d.Data.Projections.Settle(args.Context(), args.Principal.TenantID, data.Projector)
		ids := func(entity string) (map[string]bool, error) {
			res, err := data.Find(args, d.Data, data.Query{Entity: entity, Fields: []string{"name"}})
			if err != nil {
				return nil, err
			}
			out := map[string]bool{}
			for _, rec := range res.Records {
				var id string
				_ = json.Unmarshal(rec["_id"], &id)
				out[id] = true
			}
			return out, nil
		}
		insts, err := ids(controlplane.EntityInstance)
		if err != nil {
			return nil, err
		}
		hosts, err := ids(controlplane.EntityHost)
		if err != nil {
			return nil, err
		}
		var out []infra.Resource
		for id := range insts {
			if r, ok := byRecord[id]; ok && r.Kind == infra.KindInstance {
				out = append(out, r)
			} else if !ok {
				out = append(out, infra.Resource{ID: id, Kind: infra.KindInstance, Props: map[string]any{}, HoldsData: true})
			}
		}
		for _, c := range clusters {
			for _, h := range c.DependsOn {
				if hosts[infranodes.RecordID(h)] {
					out = append(out, c)
					break
				}
			}
		}
		return out, nil
	}
}
