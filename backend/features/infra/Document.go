package infra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/SgtFlame/foundation/backend/features"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/live"
)

// Document is a whole infrastructure configuration (forge 22a): the draft
// as people edit it, and each committed version's frozen copy. Every list
// is keyed by its items' ids, which never change.
type Document struct {
	Accounts   []Account   `json:"accounts"`
	Sites      []Site      `json:"sites"`
	Clusters   []Cluster   `json:"clusters"`
	HostPools  []HostPool  `json:"hostPools"`
	Profiles   []Profile   `json:"profiles"`
	Stacks     []Stack     `json:"stacks"`
	Placements []Placement `json:"placements"`
	Overrides  []Override  `json:"overrides"`
	Settings   Settings    `json:"settings"`
}

// Account is a provider account: a connection to something that makes
// hosts. Its credentials stay in the connection, never here.
type Account struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`             // lxd, azure, digitalocean, fly, infratest
	Connection string `json:"connection,omitempty"` // the connection's id
}

// Site is where a cluster is: an account and region, or an on-premise
// location (no account).
type Site struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Account   string `json:"account,omitempty"`
	Region    string `json:"region,omitempty"`
	OnPremise bool   `json:"onPremise,omitempty"`
	Location  string `json:"location,omitempty"`
}

// Cluster is the one LXD cluster of a site.
type Cluster struct {
	ID           string `json:"id"`
	Site         string `json:"site"`
	Storage      string `json:"storage,omitempty"` // zfs (the default), ceph, dir
	Network      string `json:"network,omitempty"`
	NatsReplicas int    `json:"natsReplicas,omitempty"` // 0: three, or fewer with fewer hosts
}

// HostPool is Count hosts of one size at a site.
type HostPool struct {
	ID          string `json:"id"`
	Site        string `json:"site"`
	Size        string `json:"size"`
	Count       int    `json:"count"`
	Image       string `json:"image,omitempty"`
	RequiresVMs bool   `json:"requiresVms,omitempty"` // only sizes that can run VMs
}

// Profile is what an instance is.
type Profile struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // container or vm
	Image    string   `json:"image,omitempty"`
	CPU      int      `json:"cpu,omitempty"`
	MemoryMB int      `json:"memoryMb,omitempty"`
	DiskGB   int      `json:"diskGb,omitempty"`
	Packages []string `json:"packages,omitempty"`
	Units    []Unit   `json:"units,omitempty"`
	Files    []File   `json:"files,omitempty"`
}

// Unit is a systemd unit an instance runs; a serve unit names its service
// set (forge 22i).
type Unit struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind,omitempty"` // service (the default) or serve
	Services []string `json:"services,omitempty"`
	Exec     string   `json:"exec,omitempty"`
}

// File is a file an instance holds.
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    string `json:"mode,omitempty"`
}

// Stack is a named set of profiles started together (forge 22j).
type Stack struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Profiles    []string        `json:"profiles"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Placement is where a stack, or a single profile, runs and how many.
type Placement struct {
	ID          string      `json:"id"`
	Stack       string      `json:"stack,omitempty"`
	Profile     string      `json:"profile,omitempty"`
	Sites       []SiteCount `json:"sites"`
	RequiresVMs bool        `json:"requiresVms,omitempty"`
}

// SiteCount is a number of instances at a site.
type SiteCount struct {
	Site  string `json:"site"`
	Count int    `json:"count"`
}

// Override is what one tenant gets of its own (forge 22k).
type Override struct {
	ID     string          `json:"id"`
	Tenant uint64          `json:"tenant,string"`
	Spec   json.RawMessage `json:"spec,omitempty"`
}

// OverrideSpec is an override's switches (iteration question 7, Tony
// 2026-11-27: each a separate switch). Built so far (forge 22k): runners,
// the tenant's own instances of a profile or stack at sites, in VMs of
// their own with vm; cluster, a site whose cluster is the tenant's alone;
// streams, the site whose NATS cluster holds the tenant's JetStream
// streams.
type OverrideSpec struct {
	Runners *TenantRunners `json:"runners,omitempty"`
	Cluster *TenantSite    `json:"cluster,omitempty"`
	Streams *TenantSite    `json:"streams,omitempty"`
}

// TenantSite names a site for a switch.
type TenantSite struct {
	Site string `json:"site"`
}

// OverrideShape is the override spec as a problem or tool describes it.
const OverrideShape = "{runners?: {profile | stack, sites: [{site, count}], vm?}, cluster?: {site}, streams?: {site}}"

// TenantRunners are a tenant's own instances.
type TenantRunners struct {
	Profile string      `json:"profile,omitempty"`
	Stack   string      `json:"stack,omitempty"`
	Sites   []SiteCount `json:"sites"`
	VM      bool        `json:"vm,omitempty"` // each in an LXD VM of its own, so only on hosts that can run VMs
}

// Switches reads the override's spec.
func (o Override) Switches() (OverrideSpec, error) {
	var s OverrideSpec
	if len(o.Spec) == 0 || string(o.Spec) == "null" {
		return s, nil
	}
	dec := json.NewDecoder(bytes.NewReader(o.Spec))
	dec.DisallowUnknownFields()
	err := dec.Decode(&s)
	return s, err
}

// Settings are the configuration's own settings.
type Settings struct {
	OnDrift string `json:"onDrift,omitempty"` // report (the default) or correct
	// CrossSite are the services a serve unit may call on another site,
	// across a gateway (forge 22i); every other call stays on its site.
	CrossSite []string `json:"crossSite,omitempty"`
}

// Equal tells whether two settings are the same.
func (s Settings) Equal(o Settings) bool { return reflect.DeepEqual(s, o) }

// Kinds are the document's keyed lists, by the name commands use.
var Kinds = []string{"accounts", "sites", "clusters", "hostPools", "profiles", "stacks", "placements", "overrides"}

// Providers are the provider names an account may have.
var Providers = []string{"lxd", "azure", "digitalocean", "fly", "infratest"}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidID reports whether id may name a configuration item: lower case
// letters, digits and dashes, as it becomes part of resource ids and host
// names.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Normalized is d with every list sorted by id and nil lists empty, so the
// same configuration always marshals the same, whatever order it was made
// in.
func (d Document) Normalized() Document {
	byID := func(n int, id func(int) string, swap func(int, int)) {
		sort.Sort(sorter{n: n, less: func(i, j int) bool { return id(i) < id(j) }, swap: swap})
	}
	byID(len(d.Accounts), func(i int) string { return d.Accounts[i].ID }, func(i, j int) { d.Accounts[i], d.Accounts[j] = d.Accounts[j], d.Accounts[i] })
	byID(len(d.Sites), func(i int) string { return d.Sites[i].ID }, func(i, j int) { d.Sites[i], d.Sites[j] = d.Sites[j], d.Sites[i] })
	byID(len(d.Clusters), func(i int) string { return d.Clusters[i].ID }, func(i, j int) { d.Clusters[i], d.Clusters[j] = d.Clusters[j], d.Clusters[i] })
	byID(len(d.HostPools), func(i int) string { return d.HostPools[i].ID }, func(i, j int) { d.HostPools[i], d.HostPools[j] = d.HostPools[j], d.HostPools[i] })
	byID(len(d.Profiles), func(i int) string { return d.Profiles[i].ID }, func(i, j int) { d.Profiles[i], d.Profiles[j] = d.Profiles[j], d.Profiles[i] })
	byID(len(d.Stacks), func(i int) string { return d.Stacks[i].ID }, func(i, j int) { d.Stacks[i], d.Stacks[j] = d.Stacks[j], d.Stacks[i] })
	byID(len(d.Placements), func(i int) string { return d.Placements[i].ID }, func(i, j int) { d.Placements[i], d.Placements[j] = d.Placements[j], d.Placements[i] })
	byID(len(d.Overrides), func(i int) string { return d.Overrides[i].ID }, func(i, j int) { d.Overrides[i], d.Overrides[j] = d.Overrides[j], d.Overrides[i] })
	if d.Accounts == nil {
		d.Accounts = []Account{}
	}
	if d.Sites == nil {
		d.Sites = []Site{}
	}
	if d.Clusters == nil {
		d.Clusters = []Cluster{}
	}
	if d.HostPools == nil {
		d.HostPools = []HostPool{}
	}
	if d.Profiles == nil {
		d.Profiles = []Profile{}
	}
	if d.Stacks == nil {
		d.Stacks = []Stack{}
	}
	if d.Placements == nil {
		d.Placements = []Placement{}
	}
	if d.Overrides == nil {
		d.Overrides = []Override{}
	}
	return d
}

type sorter struct {
	n    int
	less func(i, j int) bool
	swap func(i, j int)
}

func (s sorter) Len() int           { return s.n }
func (s sorter) Less(i, j int) bool { return s.less(i, j) }
func (s sorter) Swap(i, j int)      { s.swap(i, j) }

// Clone is a deep copy of d.
func (d Document) Clone() Document {
	raw, _ := json.Marshal(d)
	var out Document
	_ = json.Unmarshal(raw, &out)
	return out
}

// Item is one item of a kind as JSON, or nil.
func (d Document) Item(kind, id string) json.RawMessage {
	items, _ := d.items(kind)
	for _, it := range items {
		if itemID(it) == id {
			return it
		}
	}
	return nil
}

// items is a kind's list as raw items.
func (d Document) items(kind string) ([]json.RawMessage, error) {
	raw, _ := json.Marshal(d)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	if !isKind(kind) {
		return nil, fmt.Errorf("%q is not a kind of item: one of %s", kind, strings.Join(Kinds, ", "))
	}
	var items []json.RawMessage
	if len(m[kind]) > 0 && string(m[kind]) != "null" {
		if err := json.Unmarshal(m[kind], &items); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// With is d with item put in kind (replacing the item with its id), or
// removed when item is nil and id is given.
func (d Document) With(kind, id string, item json.RawMessage) (Document, error) {
	items, err := d.items(kind)
	if err != nil {
		return d, err
	}
	out := make([]json.RawMessage, 0, len(items)+1)
	found := false
	for _, it := range items {
		if itemID(it) == id {
			found = true
			if item != nil {
				out = append(out, item)
			}
			continue
		}
		out = append(out, it)
	}
	if !found && item != nil {
		out = append(out, item)
	}
	raw, _ := json.Marshal(d)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	m[kind], _ = json.Marshal(out)
	raw, _ = json.Marshal(m)
	var next Document
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&next); err != nil {
		return d, fmt.Errorf("the item does not fit a %s item: %v", strings.TrimSuffix(kind, "s"), err)
	}
	return next.Normalized(), nil
}

func isKind(k string) bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

func itemID(raw json.RawMessage) string {
	var it struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &it)
	return it.ID
}

// Check is every problem with d, at paths like
// document.hostPools[general].site: references that name no item, ids
// used twice, counts below zero, and a site with two clusters.
func (d Document) Check() []live.Problem {
	var ps []live.Problem
	add := func(path, msg, fix string) {
		ps = append(ps, live.Problem{Code: "bad_config", Path: "document." + path, Message: msg, Fix: fix})
	}
	seen := map[string]map[string]bool{}
	ids := func(kind string, list []string) {
		seen[kind] = map[string]bool{}
		for _, id := range list {
			switch {
			case !ValidID(id):
				add(fmt.Sprintf("%s[%s].id", kind, id), fmt.Sprintf("%q is not a valid id: lower case letters, digits and dashes, at most 63", id), "")
			case seen[kind][id]:
				add(fmt.Sprintf("%s[%s].id", kind, id), fmt.Sprintf("the id %q is used twice", id), "")
			}
			seen[kind][id] = true
		}
	}
	collect := func(n int, id func(int) string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = id(i)
		}
		return out
	}
	ids("accounts", collect(len(d.Accounts), func(i int) string { return d.Accounts[i].ID }))
	ids("sites", collect(len(d.Sites), func(i int) string { return d.Sites[i].ID }))
	ids("clusters", collect(len(d.Clusters), func(i int) string { return d.Clusters[i].ID }))
	ids("hostPools", collect(len(d.HostPools), func(i int) string { return d.HostPools[i].ID }))
	ids("profiles", collect(len(d.Profiles), func(i int) string { return d.Profiles[i].ID }))
	ids("stacks", collect(len(d.Stacks), func(i int) string { return d.Stacks[i].ID }))
	ids("placements", collect(len(d.Placements), func(i int) string { return d.Placements[i].ID }))
	ids("overrides", collect(len(d.Overrides), func(i int) string { return d.Overrides[i].ID }))
	ref := func(path, kind, id string) {
		if id != "" && !seen[kind][id] {
			add(path, fmt.Sprintf("there is no %s %q", strings.TrimSuffix(kind, "s"), id), "add it first, or name one that exists")
		}
	}
	for _, a := range d.Accounts {
		if !contains(Providers, a.Provider) {
			add(fmt.Sprintf("accounts[%s].provider", a.ID), fmt.Sprintf("%q is not a provider: one of %s", a.Provider, strings.Join(Providers, ", ")), "")
		}
	}
	for _, s := range d.Sites {
		switch {
		case s.OnPremise && s.Account != "":
			add(fmt.Sprintf("sites[%s].account", s.ID), "an on-premise site has no provider account", "leave account out, or set onPremise false")
		case !s.OnPremise && s.Account == "":
			add(fmt.Sprintf("sites[%s].account", s.ID), "a cloud site needs a provider account", "name an account, or set onPremise true")
		}
		ref(fmt.Sprintf("sites[%s].account", s.ID), "accounts", s.Account)
	}
	clusterOf := map[string]string{}
	for _, c := range d.Clusters {
		ref(fmt.Sprintf("clusters[%s].site", c.ID), "sites", c.Site)
		if other, ok := clusterOf[c.Site]; ok {
			add(fmt.Sprintf("clusters[%s].site", c.ID), fmt.Sprintf("the site %q already has the cluster %q: one LXD cluster per site", c.Site, other), "")
		}
		clusterOf[c.Site] = c.ID
		if c.Storage != "" && !contains([]string{"zfs", "ceph", "dir"}, c.Storage) {
			add(fmt.Sprintf("clusters[%s].storage", c.ID), "storage is zfs, ceph or dir", "")
		}
		if c.NatsReplicas < 0 || c.NatsReplicas > 5 {
			add(fmt.Sprintf("clusters[%s].natsReplicas", c.ID), "natsReplicas is 0 to 5", "")
		}
	}
	for _, h := range d.HostPools {
		ref(fmt.Sprintf("hostPools[%s].site", h.ID), "sites", h.Site)
		if h.Count < 0 || h.Count > 1000 {
			add(fmt.Sprintf("hostPools[%s].count", h.ID), "count is 0 to 1000", "")
		}
		if h.Size == "" {
			add(fmt.Sprintf("hostPools[%s].size", h.ID), "a host pool needs a size", "list the sizes with infra.compute's size.list")
		}
	}
	for _, p := range d.Profiles {
		if p.Kind != "container" && p.Kind != "vm" {
			add(fmt.Sprintf("profiles[%s].kind", p.ID), "kind is container or vm", "")
		}
		units := map[string]bool{}
		for i, u := range p.Units {
			if u.Name == "" || units[u.Name] {
				add(fmt.Sprintf("profiles[%s].units.%d.name", p.ID, i), "every unit needs a name of its own", "")
			}
			units[u.Name] = true
			if u.Kind == "serve" && len(u.Services) == 0 {
				add(fmt.Sprintf("profiles[%s].units.%d.services", p.ID, i), "a serve unit names its service set", "for example [\"runner\"]")
			}
		}
	}
	for _, s := range d.Stacks {
		for i, p := range s.Profiles {
			ref(fmt.Sprintf("stacks[%s].profiles.%d", s.ID, i), "profiles", p)
		}
	}
	for _, pl := range d.Placements {
		if (pl.Stack == "") == (pl.Profile == "") {
			add(fmt.Sprintf("placements[%s]", pl.ID), "a placement names a stack or a profile, not both", "")
		}
		ref(fmt.Sprintf("placements[%s].stack", pl.ID), "stacks", pl.Stack)
		ref(fmt.Sprintf("placements[%s].profile", pl.ID), "profiles", pl.Profile)
		for i, sc := range pl.Sites {
			ref(fmt.Sprintf("placements[%s].sites.%d.site", pl.ID, i), "sites", sc.Site)
			if sc.Count < 0 || sc.Count > 1000 {
				add(fmt.Sprintf("placements[%s].sites.%d.count", pl.ID, i), "count is 0 to 1000", "")
			}
		}
	}
	clusterAt := map[string]bool{}
	for _, c := range d.Clusters {
		clusterAt[c.Site] = true
	}
	dedicated := map[string]uint64{} // site → the tenant whose own cluster it is
	streamsOf := map[uint64]string{}
	for _, o := range d.Overrides {
		path := fmt.Sprintf("overrides[%s]", o.ID)
		if sw, err := o.Switches(); err == nil && sw.Streams != nil {
			if prev, ok := streamsOf[o.Tenant]; ok && prev != sw.Streams.Site {
				add(path+".spec.streams", fmt.Sprintf("tenant %d's streams are placed at %s by another override", o.Tenant, prev), "")
			}
			streamsOf[o.Tenant] = sw.Streams.Site
		}
		if o.Tenant == 0 || o.Tenant == features.RootTenantID {
			add(path+".tenant", "an override names a tenant other than the root tenant", "")
		}
		sw, err := o.Switches()
		if err != nil {
			add(path+".spec", "the spec is "+OverrideShape+": "+err.Error(), "")
			continue
		}
		if c := sw.Cluster; c != nil {
			ref(path+".spec.cluster.site", "sites", c.Site)
			if c.Site == "" {
				add(path+".spec.cluster.site", "cluster names the site whose cluster is the tenant's own", "")
			} else if !clusterAt[c.Site] {
				add(path+".spec.cluster.site", fmt.Sprintf("site %s has no cluster", c.Site), "add a cluster to "+c.Site)
			} else if other, ok := dedicated[c.Site]; ok && other != o.Tenant {
				add(path+".spec.cluster.site", fmt.Sprintf("site %s's cluster is tenant %d's own already", c.Site, other), "")
			} else {
				dedicated[c.Site] = o.Tenant
			}
			if r := sw.Runners; r != nil {
				for i, sc := range r.Sites {
					if sc.Site != c.Site {
						add(fmt.Sprintf("%s.spec.runners.sites.%d.site", path, i), fmt.Sprintf("the tenant's runners go on its own cluster at %s", c.Site), "")
					}
				}
			}
		}
		if st := sw.Streams; st != nil {
			ref(path+".spec.streams.site", "sites", st.Site)
			if st.Site == "" {
				add(path+".spec.streams.site", "streams names the site whose NATS cluster holds the tenant's streams", "")
			} else if !clusterAt[st.Site] {
				add(path+".spec.streams.site", fmt.Sprintf("site %s has no cluster, so no NATS", st.Site), "add a cluster to "+st.Site)
			}
		}
		if r := sw.Runners; r != nil {
			if (r.Stack == "") == (r.Profile == "") {
				add(path+".spec.runners", "runners name a stack or a profile, not both", "")
			}
			ref(path+".spec.runners.stack", "stacks", r.Stack)
			ref(path+".spec.runners.profile", "profiles", r.Profile)
			for i, sc := range r.Sites {
				ref(fmt.Sprintf("%s.spec.runners.sites.%d.site", path, i), "sites", sc.Site)
				if sc.Count < 0 || sc.Count > 1000 {
					add(fmt.Sprintf("%s.spec.runners.sites.%d.count", path, i), "count is 0 to 1000", "")
				}
			}
		}
	}
	if d.Settings.OnDrift != "" && d.Settings.OnDrift != "report" && d.Settings.OnDrift != "correct" {
		add("settings.onDrift", "onDrift is report or correct", "")
	}
	return ps
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
