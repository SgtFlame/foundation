package infraapply

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	infranodes "github.com/SgtFlame/foundation/backend/features/infra/nodes"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// TopologyView is the infra.topology query's result: the applied version
// (else the head, else the draft) as resources by site, each with the
// state of the Control Plane record it is made as, live (forge 22p).
type TopologyView struct {
	Config  string         `json:"config"`
	Version int            `json:"version"` // 0: the draft
	Applied int            `json:"applied"`
	Head    int            `json:"head"`
	Sites   []TopologySite `json:"sites"`
	Counts  map[string]int `json:"counts"`
}

// TopologySite is one site with its resources in kind order.
type TopologySite struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Region    string          `json:"region,omitempty"`
	OnPremise bool            `json:"onPremise,omitempty"`
	Resources []TopologyEntry `json:"resources"`
}

// TopologyEntry is one resource: its id and kind, the record it is made
// as, its state (present, missing, extra) and the record's health and
// status.
type TopologyEntry struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Record string `json:"record,omitempty"`
	Host   string `json:"host,omitempty"` // an instance's host record
	State  string `json:"state"`
	Health string `json:"health,omitempty"`
	Status string `json:"status,omitempty"`
}

const topologyFields = "{config, version (0: the draft), applied, head, sites: [{id, name, region, onPremise, resources: [{id, kind, name, record, host, state: present | missing | extra, health: up | down | unknown, status}]}], counts: {present, missing, extra}}"

func contributeTopology(reg *extensions.Registry) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.topology", Source: source}, Title: "Infrastructure topology",
		Description: "The configuration's applied version (else its last committed one, else the draft) as resources by site, each with the state of the Control Plane record it is made as (present, missing; extra for a record no resource names) and that record's health and status, following the configuration and the records live: " + topologyFields,
		Params:      json.RawMessage(`{"type":"object","properties":{"config":{"type":"string","description":"The configuration, main when left out"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      infra.ActionRead,
		Run:         func(f live.Feed) error { return runTopology(reg, f) },
	})
}

func runTopology(reg *extensions.Registry, f live.Feed) error {
	var p struct{ Config string }
	_ = json.Unmarshal(f.Params(), &p)
	if p.Config == "" || !live.ValidToken(p.Config) {
		p.Config = infra.DefaultConfig
	}
	tenant := f.Args().Principal.TenantID
	type watch struct {
		projector, pattern string
		values             map[string]json.RawMessage
	}
	watches := []*watch{
		{projector: infra.ConfigProjector, pattern: infra.ConfigType + "." + p.Config},
		{projector: infra.VersionProjector, pattern: infra.VersionType + ".*"},
		{projector: data.Projector, pattern: controlplane.EntityHost + ".*"},
		{projector: data.Projector, pattern: controlplane.EntityInstance + ".*"},
		{projector: data.Projector, pattern: controlplane.EntityNatsCluster + ".*"},
	}
	type update struct {
		w *watch
		e *live.Entry
	}
	updates := make(chan update)
	for _, w := range watches {
		w.values = map[string]json.RawMessage{}
		ch, err := f.Store().Watch(f.Context(), tenant, w.projector, w.pattern)
		if err != nil {
			return err
		}
		go func(w *watch, ch <-chan *live.Entry) {
			for e := range ch {
				select {
				case updates <- update{w, e}:
				case <-f.Context().Done():
					return
				}
			}
		}(w, ch)
	}
	started := 0
	pos := live.Position{}
	var last []byte
	for {
		select {
		case <-f.Context().Done():
			return nil
		case u := <-updates:
			if u.e == nil {
				started++
			} else {
				pos = pos.Merge(u.e.Value.Position())
				if u.e.Deleted {
					delete(u.w.values, u.e.Key)
				} else {
					u.w.values[u.e.Key] = u.e.Value.Value
				}
			}
			if started < len(watches) {
				continue
			}
			view := Topology(reg, p.Config, watches[0].values[infra.ConfigType+"."+p.Config], watches[1].values, watches[2].values, watches[3].values, watches[4].values)
			raw, _ := json.Marshal(view)
			switch {
			case last == nil:
				f.Snapshot(view, pos)
			case string(raw) != string(last):
				f.Change(live.Delta{Op: "replace", Value: raw}, pos)
			default:
				f.Change(live.Delta{Op: "reached"}, pos)
			}
			last = raw
		}
	}
}

// Topology computes the view from the configuration's value, its
// versions and the host, instance and NATS cluster records, each keyed as
// projected.
func Topology(reg *extensions.Registry, config string, cfgRaw json.RawMessage, versions, hosts, instances, natsClusters map[string]json.RawMessage) TopologyView {
	view := TopologyView{Config: config, Sites: []TopologySite{}, Counts: map[string]int{}}
	var cfg infra.Config
	_ = json.Unmarshal(cfgRaw, &cfg)
	view.Applied, view.Head = cfg.Applied, cfg.Head
	doc := cfg.Draft
	view.Version = cfg.Applied
	if view.Version == 0 {
		view.Version = cfg.Head
	}
	if view.Version > 0 {
		var v infra.Version
		if raw, ok := versions[infra.VersionType+"."+infra.VersionID(config, view.Version)]; ok && json.Unmarshal(raw, &v) == nil {
			doc = v.Document
		} else {
			view.Version = 0 // not folded yet: the draft meanwhile
		}
	}
	records := func(m map[string]json.RawMessage, entity string) map[string]map[string]any {
		out := map[string]map[string]any{}
		for k, raw := range m {
			var r map[string]any
			if json.Unmarshal(raw, &r) == nil {
				out[strings.TrimPrefix(k, entity+".")] = r
			}
		}
		return out
	}
	hostRecs, instRecs := records(hosts, controlplane.EntityHost), records(instances, controlplane.EntityInstance)
	natsRecs := map[string]map[string]json.RawMessage{}
	for k, raw := range natsClusters {
		var r map[string]json.RawMessage
		if json.Unmarshal(raw, &r) == nil {
			natsRecs[strings.TrimPrefix(k, controlplane.EntityNatsCluster+".")] = r
		}
	}
	str := func(r map[string]any, f string) string { s, _ := r[f].(string); return s }

	norm := doc.Normalized()
	bySite := map[string]int{}
	site := func(id string) *TopologySite {
		if i, ok := bySite[id]; ok {
			return &view.Sites[i]
		}
		view.Sites = append(view.Sites, TopologySite{ID: id, Name: id, Resources: []TopologyEntry{}})
		bySite[id] = len(view.Sites) - 1
		return &view.Sites[len(view.Sites)-1]
	}
	for _, s := range norm.Sites {
		ts := site(s.ID)
		ts.Name, ts.Region, ts.OnPremise = s.Name, s.Region, s.OnPremise
	}
	desired, kinds := infra.Expand(reg, doc)
	order := map[string]int{}
	for _, k := range kinds {
		order[k.Kind] = k.Order
	}
	named := map[string]bool{}
	presentHosts := map[string]bool{} // by site
	for _, r := range desired {
		e := TopologyEntry{ID: r.ID, Kind: r.Kind, Name: r.ID, State: infra.StateMissing}
		var rec map[string]any
		switch r.Kind {
		case "host":
			e.Record = infranodes.RecordID(r.ID)
			rec = hostRecs[e.Record]
		case infra.KindInstance, infra.KindNatsNode:
			e.Record = infranodes.RecordID(r.ID)
			rec = instRecs[e.Record]
		case infra.KindGateway:
			if Joined(r, natsRecs) {
				e.State = infra.StatePresent
			}
		}
		if e.Record != "" {
			named[e.Record] = true
		}
		if n, _ := r.Props["name"].(string); n != "" {
			e.Name = n
		}
		if rec != nil {
			e.State = infra.StatePresent
			e.Health, e.Status, e.Host = str(rec, "health"), str(rec, "status"), str(rec, "host")
			if r.Kind == "host" {
				presentHosts[r.Site] = true
			}
		}
		site(r.Site).Resources = append(site(r.Site).Resources, e)
	}
	// A cluster is there once one of its hosts is (clusters are formed in 22h).
	for i := range view.Sites {
		for j, e := range view.Sites[i].Resources {
			if e.Kind == "cluster" && presentHosts[view.Sites[i].ID] {
				view.Sites[i].Resources[j].State = infra.StatePresent
			}
		}
	}
	for id, r := range hostRecs {
		if !named[id] && strings.HasPrefix(id, "host_") {
			e := TopologyEntry{ID: id, Kind: "host", Name: str(r, "name"), Record: id, State: infra.StateExtra, Health: str(r, "health"), Status: str(r, "status")}
			site(str(r, "site")).Resources = append(site(str(r, "site")).Resources, e)
		}
	}
	for id, r := range instRecs {
		if !named[id] && strings.HasPrefix(id, "instance_") {
			s := ""
			if h := hostRecs[str(r, "host")]; h != nil {
				s = str(h, "site")
			}
			e := TopologyEntry{ID: id, Kind: "instance", Name: str(r, "name"), Record: id, Host: str(r, "host"), State: infra.StateExtra, Health: str(r, "health"), Status: str(r, "status")}
			site(s).Resources = append(site(s).Resources, e)
		}
	}
	for i := range view.Sites {
		rs := view.Sites[i].Resources
		sort.SliceStable(rs, func(a, b int) bool {
			if order[rs[a].Kind] != order[rs[b].Kind] {
				return order[rs[a].Kind] < order[rs[b].Kind]
			}
			return rs[a].ID < rs[b].ID
		})
		for _, e := range rs {
			view.Counts[e.State]++
		}
	}
	return view
}
