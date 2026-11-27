package infra

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Resource is one thing a configuration makes (forge 22d), with a stable
// id derived from the configuration (host:west/general/2,
// cluster:west, instance:west/web/web/1), its desired properties and the
// resources it depends on. Plan compares desired resources with actual
// ones by id.
type Resource struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Site      string         `json:"site,omitempty"`
	Account   string         `json:"account,omitempty"` // a host's provider account; empty on premise
	Props     map[string]any `json:"props"`
	DependsOn []string       `json:"dependsOn,omitempty"`
	NeedsVMs  bool           `json:"needsVms,omitempty"`
	HoldsData bool           `json:"holdsData,omitempty"`
}

// ResourceKind is one kind of resource, contributed on ResourceKinds by
// the part that adds it (hosts, clusters and instances here; stacks,
// service units and overrides later): Expand gives a document's resources
// of the kind, Compared names the properties a change of which is a
// change (all when empty), and Order sorts kinds for creation (removals
// run in reverse).
type ResourceKind struct {
	extensions.Descriptor
	Kind     string
	Title    string
	Order    int
	Compared []string
	// HoldsData marks the kind's resources as holding data, so removing
	// one asks first (22f).
	HoldsData bool
	Expand    func(doc Document) []Resource
}

// ResourceKinds is the infra.resourceKind point.
var ResourceKinds = extensions.NewPoint[ResourceKind]("infra.resourceKind", "A kind of infrastructure resource a configuration expands into (forge 22d): its expansion, compared properties and order")

// Resource kinds built in.
const (
	KindHost     = "host"
	KindCluster  = "cluster"
	KindInstance = "instance"
)

// HostID is the resource id of a host pool's i-th host (from 1).
func HostID(site, pool string, i int) string {
	return "host:" + site + "/" + pool + "/" + strconv.Itoa(i)
}

// ProviderName is the name a host is made under at its provider, and the
// id infra.compute knows it by: site-pool-i.
func ProviderName(site, pool string, i int) string {
	return site + "-" + pool + "-" + strconv.Itoa(i)
}

// ClusterID is a site's cluster's resource id.
func ClusterID(site string) string { return "cluster:" + site }

// InstanceID is a placement's i-th instance of a profile at a site.
func InstanceID(site, placement, profile string, i int) string {
	return "instance:" + site + "/" + placement + "/" + profile + "/" + strconv.Itoa(i)
}

func propsOf(v any) map[string]any {
	raw, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func siteMap(doc Document) map[string]Site {
	out := map[string]Site{}
	for _, s := range doc.Sites {
		out[s.ID] = s
	}
	return out
}

func expandHosts(doc Document) []Resource {
	sites := siteMap(doc)
	var out []Resource
	for _, p := range doc.HostPools {
		s := sites[p.Site]
		for i := 1; i <= p.Count; i++ {
			props := map[string]any{"name": ProviderName(p.Site, p.ID, i), "size": p.Size, "region": s.Region, "pool": p.ID}
			if p.Image != "" {
				props["image"] = p.Image
			}
			if s.OnPremise {
				props["onPremise"] = true
			}
			out = append(out, Resource{ID: HostID(p.Site, p.ID, i), Kind: KindHost, Site: p.Site, Account: s.Account, Props: props, NeedsVMs: p.RequiresVMs})
		}
	}
	return out
}

func hostsAt(doc Document, site string) []string {
	var out []string
	for _, p := range doc.HostPools {
		if p.Site == site {
			for i := 1; i <= p.Count; i++ {
				out = append(out, HostID(site, p.ID, i))
			}
		}
	}
	return out
}

func expandClusters(doc Document) []Resource {
	var out []Resource
	for _, c := range doc.Clusters {
		props := propsOf(c)
		delete(props, "id")
		delete(props, "site")
		out = append(out, Resource{ID: ClusterID(c.Site), Kind: KindCluster, Site: c.Site, Props: props, DependsOn: hostsAt(doc, c.Site), HoldsData: true})
	}
	return out
}

func expandInstances(doc Document) []Resource {
	profiles := map[string]Profile{}
	for _, p := range doc.Profiles {
		profiles[p.ID] = p
	}
	stacks := map[string]Stack{}
	for _, s := range doc.Stacks {
		stacks[s.ID] = s
	}
	clusters := map[string]bool{}
	for _, c := range doc.Clusters {
		clusters[c.Site] = true
	}
	var out []Resource
	for _, pl := range doc.Placements {
		names := []string{pl.Profile}
		if pl.Stack != "" {
			names = stacks[pl.Stack].Profiles
		}
		for _, sc := range pl.Sites {
			deps := hostsAt(doc, sc.Site)
			if clusters[sc.Site] {
				deps = []string{ClusterID(sc.Site)}
			}
			for _, name := range names {
				prof, ok := profiles[name]
				if !ok {
					continue
				}
				props := propsOf(prof)
				delete(props, "id")
				delete(props, "name")
				props["profile"] = name
				for i := 1; i <= sc.Count; i++ {
					out = append(out, Resource{ID: InstanceID(sc.Site, pl.ID, name, i), Kind: KindInstance, Site: sc.Site, Props: props, DependsOn: deps,
						NeedsVMs: prof.Kind == "vm" || pl.RequiresVMs, HoldsData: true})
				}
			}
		}
	}
	return out
}

func contributeResources(reg *extensions.Registry) {
	ResourceKinds.Register(reg, ResourceKind{Descriptor: extensions.Descriptor{ID: KindHost, Source: source}, Kind: KindHost, Title: "Host", Order: 10,
		Compared: []string{"size", "region", "image"}, Expand: expandHosts})
	ResourceKinds.Register(reg, ResourceKind{Descriptor: extensions.Descriptor{ID: KindCluster, Source: source}, Kind: KindCluster, Title: "Cluster", Order: 20, HoldsData: true, Expand: expandClusters})
	ResourceKinds.Register(reg, ResourceKind{Descriptor: extensions.Descriptor{ID: KindInstance, Source: source}, Kind: KindInstance, Title: "Instance", Order: 30, HoldsData: true, Expand: expandInstances})
}

// Expand is a document's resources, by every kind on the registry, sorted
// by kind order then id.
func Expand(reg *extensions.Registry, doc Document) ([]Resource, map[string]ResourceKind) {
	kinds := map[string]ResourceKind{}
	var out []Resource
	for _, k := range ResourceKinds.All(reg) {
		kinds[k.Kind] = k
		out = append(out, k.Expand(doc.Normalized())...)
	}
	sortResources(out, kinds)
	return out, kinds
}

func sortResources(rs []Resource, kinds map[string]ResourceKind) {
	sort.SliceStable(rs, func(i, j int) bool {
		oi, oj := kinds[rs[i].Kind].Order, kinds[rs[j].Kind].Order
		if oi != oj {
			return oi < oj
		}
		return rs[i].ID < rs[j].ID
	})
}

func describeKind(kinds map[string]ResourceKind, kind string) string {
	if k, ok := kinds[kind]; ok && k.Title != "" {
		return k.Title
	}
	return fmt.Sprint(kind)
}

func sortStrings(s []string) { sort.Strings(s) }
