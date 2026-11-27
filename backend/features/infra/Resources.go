package infra

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
	KindNatsNode = "natsNode"
	KindGateway  = "gateway"
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

// NatsNodeID is a site's n-th NATS node (from 1).
func NatsNodeID(site string, n int) string {
	return "natsNode:" + site + "/" + strconv.Itoa(n)
}

// NatsInstanceName is the instance a site's n-th NATS node runs in, the
// name its peers and other sites reach it at.
func NatsInstanceName(site string, n int) string {
	return "nats-" + site + "-" + strconv.Itoa(n)
}

// GatewayID joins two sites' NATS clusters, the sites in id order.
func GatewayID(a, b string) string {
	if b < a {
		a, b = b, a
	}
	return "gateway:" + a + "~" + b
}

// NatsUnitRecord is the record id of the nats unit a site's n-th node
// runs, as Ensure unit makes it from the node's instance record.
func NatsUnitRecord(site string, n int) string {
	return RecordID(NatsNodeID(site, n)) + "__nats"
}

// NatsClusterRecord is the record id of a site's infraNatsCluster.
func NatsClusterRecord(site string) string { return RecordID("nats:" + site) }

// RecordID is the desired-state record id of a resource id: its words
// joined by _, which no configuration id holds, so ids never collide; a
// long one is cut and given a hash.
func RecordID(resource string) string {
	id := strings.NewReplacer(":", "_", "/", "_", ".", "_").Replace(resource)
	if len(id) > 120 {
		sum := sha256.Sum256([]byte(resource))
		id = id[:100] + "_" + hex.EncodeToString(sum[:8])
	}
	return id
}

// natsCount is how many NATS nodes a cluster runs: its natsReplicas, else
// three, never more than its hosts.
func natsCount(c Cluster, hosts int) int {
	n := c.NatsReplicas
	if n == 0 {
		n = 3
	}
	if n > hosts {
		n = hosts
	}
	return n
}

func natsURLs(scheme, site string, count, port int) []any {
	out := []any{}
	for n := 1; n <= count; n++ {
		out = append(out, scheme+"://"+NatsInstanceName(site, n)+":"+strconv.Itoa(port))
	}
	return out
}

// natsTopology is each clustered site's NATS cluster as its nodes write
// it (forge 22h): the nodes' unit records, the gateways to every other
// cloud site and, for an on-premise site, a leaf remote to the first
// cloud site (Tony to confirm: iteration question 4).
type natsSite struct {
	Count     int
	OnPremise bool
	Hosts     []string
}

func natsSites(doc Document) (map[string]natsSite, []string) {
	sites := siteMap(doc)
	out := map[string]natsSite{}
	var ids []string
	for _, c := range doc.Clusters {
		hosts := hostsAt(doc, c.Site)
		n := natsCount(c, len(hosts))
		if n == 0 {
			continue
		}
		out[c.Site] = natsSite{Count: n, OnPremise: sites[c.Site].OnPremise, Hosts: hosts}
		ids = append(ids, c.Site)
	}
	sort.Strings(ids)
	return out, ids
}

// NatsClusterValues is the infraNatsCluster record a site's NATS nodes
// write, from the document: name, site, nodes, gateways, leafRemotes.
func natsClusterValues(site string, all map[string]natsSite, ids []string) map[string]any {
	me := all[site]
	nodes := []any{}
	for n := 1; n <= me.Count; n++ {
		nodes = append(nodes, NatsUnitRecord(site, n))
	}
	gateways, leaf := []any{}, []any{}
	for _, o := range ids {
		os := all[o]
		if o == site || os.OnPremise {
			continue
		}
		if me.OnPremise {
			if len(leaf) == 0 {
				urls := natsURLs("nats-leaf", o, os.Count, 7422)
				leaf = append(leaf, map[string]any{"url": urls[0], "urls": urls, "hub": o})
			}
			continue
		}
		gateways = append(gateways, map[string]any{"name": o, "urls": natsURLs("nats", o, os.Count, 7222)})
	}
	return map[string]any{"name": site, "site": site, "nodes": nodes, "gateways": gateways, "leafRemotes": leaf}
}

func expandNatsNodes(doc Document) []Resource {
	all, ids := natsSites(doc)
	var out []Resource
	for _, site := range ids {
		s := all[site]
		cluster := natsClusterValues(site, all, ids)
		for n := 1; n <= s.Count; n++ {
			host := s.Hosts[(n-1)%len(s.Hosts)]
			props := map[string]any{"name": NatsInstanceName(site, n), "host": host, "record": NatsClusterRecord(site),
				"peers": cluster["nodes"], "gateways": cluster["gateways"], "leafRemotes": cluster["leafRemotes"]}
			out = append(out, Resource{ID: NatsNodeID(site, n), Kind: KindNatsNode, Site: site, Props: props,
				DependsOn: []string{ClusterID(site), host}, HoldsData: true})
		}
	}
	return out
}

func expandGateways(doc Document) []Resource {
	all, ids := natsSites(doc)
	var out []Resource
	hub := ""
	for _, id := range ids {
		if !all[id].OnPremise {
			hub = id
			break
		}
	}
	nodes := func(site string) []string {
		var d []string
		for n := 1; n <= all[site].Count; n++ {
			d = append(d, NatsNodeID(site, n))
		}
		return d
	}
	for i, a := range ids {
		for _, b := range ids[i+1:] {
			mode := "gateway"
			switch {
			case all[a].OnPremise && all[b].OnPremise:
				continue
			case all[a].OnPremise || all[b].OnPremise:
				if a != hub && b != hub {
					continue
				}
				mode = "leaf"
			}
			out = append(out, Resource{ID: GatewayID(a, b), Kind: KindGateway, Site: a,
				Props:     map[string]any{"mode": mode, "sites": []any{a, b}},
				DependsOn: append(nodes(a), nodes(b)...)})
		}
	}
	return out
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
	ResourceKinds.Register(reg, ResourceKind{Descriptor: extensions.Descriptor{ID: KindNatsNode, Source: source}, Kind: KindNatsNode, Title: "NATS node", Order: 22, HoldsData: true,
		Compared: []string{"host", "peers", "gateways", "leafRemotes"}, Expand: expandNatsNodes})
	ResourceKinds.Register(reg, ResourceKind{Descriptor: extensions.Descriptor{ID: KindGateway, Source: source}, Kind: KindGateway, Title: "Gateway", Order: 24,
		Compared: []string{"mode"}, Expand: expandGateways})
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
