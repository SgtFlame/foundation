package infraapply

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
		var clusters, gateways []infra.Resource
		for _, doc := range docs {
			rs, _ := infra.Expand(reg, doc)
			for _, r := range rs {
				byRecord[infranodes.RecordID(r.ID)] = r
				switch r.Kind {
				case infra.KindCluster:
					clusters = append(clusters, r)
				case infra.KindGateway:
					gateways = append(gateways, r)
				}
			}
		}
		_ = d.Data.Projections.Settle(args.Context(), args.Principal.TenantID, data.Projector)
		recs := func(entity string, fields ...string) (map[string]map[string]json.RawMessage, error) {
			res, err := data.Find(args, d.Data, data.Query{Entity: entity, Fields: append([]string{"name"}, fields...)})
			if err != nil {
				return nil, err
			}
			out := map[string]map[string]json.RawMessage{}
			for _, rec := range res.Records {
				var id string
				_ = json.Unmarshal(rec["_id"], &id)
				out[id] = rec
			}
			return out, nil
		}
		insts, err := recs(controlplane.EntityInstance, "host")
		if err != nil {
			return nil, err
		}
		hosts, err := recs(controlplane.EntityHost)
		if err != nil {
			return nil, err
		}
		natsRecs, err := recs(controlplane.EntityNatsCluster, "nodes", "gateways", "leafRemotes")
		if err != nil {
			return nil, err
		}
		jsonOf := func(raw json.RawMessage) any {
			var v any
			if json.Unmarshal(raw, &v) != nil || v == nil {
				return []any{}
			}
			return v
		}
		var out []infra.Resource
		for id, rec := range insts {
			r, ok := byRecord[id]
			switch {
			case ok && r.Kind == infra.KindInstance:
				out = append(out, r)
			case ok && r.Kind == infra.KindNatsNode:
				// A NATS node as its records have it: its host and its
				// site's cluster record.
				a := r
				a.Props = map[string]any{}
				for k, v := range r.Props {
					a.Props[k] = v
				}
				var host string
				_ = json.Unmarshal(rec["host"], &host)
				if want, _ := r.Props["host"].(string); infranodes.RecordID(want) != host {
					a.Props["host"] = host
				}
				nr := natsRecs[fmt.Sprint(r.Props["record"])]
				a.Props["peers"], a.Props["gateways"], a.Props["leafRemotes"] = jsonOf(nr["nodes"]), jsonOf(nr["gateways"]), jsonOf(nr["leafRemotes"])
				out = append(out, a)
			case !ok:
				out = append(out, infra.Resource{ID: id, Kind: infra.KindInstance, Props: map[string]any{}, HoldsData: true})
			}
		}
		for _, c := range clusters {
			for _, h := range c.DependsOn {
				if hosts[infranodes.RecordID(h)] != nil {
					out = append(out, c)
					break
				}
			}
		}
		for _, g := range gateways {
			if Joined(g, natsRecs) {
				out = append(out, g)
			}
		}
		// A tenant's streams placement (forge 22k), from its account record.
		accts, err := recs(controlplane.EntityNatsAcct, "tenant", "placement")
		if err != nil {
			return nil, err
		}
		out = append(out, StreamsActual(accts)...)
		return out, nil
	}
}

// Joined tells whether a gateway resource's two sites are joined in their
// infraNatsCluster records (by record id): one names the other among its
// gateways, or as the hub of its leaf remotes.
func Joined(g infra.Resource, natsRecs map[string]map[string]json.RawMessage) bool {
	sites, _ := g.Props["sites"].([]any)
	if len(sites) != 2 {
		return false
	}
	names := func(a, b string) bool {
		rec := natsRecs[infra.NatsClusterRecord(a)]
		if rec == nil {
			return false
		}
		var gws, leaf []struct {
			Name string `json:"name"`
			Hub  string `json:"hub"`
		}
		_ = json.Unmarshal(rec["gateways"], &gws)
		_ = json.Unmarshal(rec["leafRemotes"], &leaf)
		for _, x := range gws {
			if x.Name == b {
				return true
			}
		}
		for _, x := range leaf {
			if x.Hub == b {
				return true
			}
		}
		return false
	}
	a, b := fmt.Sprint(sites[0]), fmt.Sprint(sites[1])
	return names(a, b) || names(b, a)
}

// StreamsActual is each tenant's streams placement as its
// infraNatsAccount record (by record id, the first for a tenant) holds it.
func StreamsActual(accts map[string]map[string]json.RawMessage) []infra.Resource {
	ids := make([]string, 0, len(accts))
	for id := range accts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	seen := map[string]bool{}
	var out []infra.Resource
	for _, id := range ids {
		var tenant, placement string
		_ = json.Unmarshal(accts[id]["tenant"], &tenant)
		_ = json.Unmarshal(accts[id]["placement"], &placement)
		tenant = strings.TrimSpace(tenant)
		n, err := strconv.ParseUint(tenant, 10, 64)
		if err != nil || seen[tenant] {
			continue
		}
		seen[tenant] = true
		if placement = strings.TrimSpace(placement); placement != "" {
			out = append(out, infra.Resource{ID: infra.StreamsID(n), Kind: infra.KindStreams, Site: placement,
				Props: map[string]any{"tenant": tenant, "cluster": placement}})
		}
	}
	return out
}
