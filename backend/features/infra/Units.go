package infra

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/services"
)

// Services are the services serve units may name and what they declare
// (forge 22i): every service's Spec with its Manifest, and the named
// groups (web, data, runner and those a server adds).
type Services struct {
	Specs  []services.Spec
	Groups services.Groups
}

// UnitAccess is what a serve unit may do, from its services' manifests:
// the services it runs, its NATS permissions in the services account and
// the Vault paths it may read. A plan shows it on the unit's instance.
type UnitAccess struct {
	Unit        string               `json:"unit"`
	Set         string               `json:"set"`
	Services    []string             `json:"services"`
	Permissions services.Permissions `json:"permissions"`
	Secrets     []string             `json:"secrets"`
}

func (sv Services) known() []string {
	out := make([]string, 0, len(sv.Specs))
	for _, s := range sv.Specs {
		out = append(out, s.Name)
	}
	return out
}

func (sv Services) spec(name string) (services.Spec, bool) {
	for _, s := range sv.Specs {
		if s.Name == name {
			return s, true
		}
	}
	return services.Spec{}, false
}

// setOf is the services a unit's set names, every known one for all.
func (sv Services) setOf(u Unit) ([]string, error) {
	spec := strings.Join(u.Services, ",")
	set, err := services.ParseSet(spec, sv.Groups, sv.known())
	if err != nil {
		return nil, err
	}
	if set == nil {
		return sv.known(), nil
	}
	out := []string{}
	for s := range set {
		if !strings.HasPrefix(s, "connectors:") {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Access is a serve unit's access, from its services' manifests.
func (sv Services) Access(u Unit) (UnitAccess, error) {
	names, err := sv.setOf(u)
	if err != nil {
		return UnitAccess{}, err
	}
	a := UnitAccess{Unit: u.Name, Set: strings.Join(u.Services, ","), Services: names, Permissions: services.ProcessPermissions(sv.Specs, names), Secrets: []string{}}
	for _, n := range names {
		if s, ok := sv.spec(n); ok {
			a.Secrets = append(a.Secrets, s.Manifest.Secrets...)
		}
	}
	sort.Strings(a.Secrets)
	a.Secrets = slices.Compact(a.Secrets)
	return a, nil
}

// CheckUnits checks every placed serve unit's calls (forge 22i): each
// service a unit runs may call only services some unit serves on the same
// site, or, when the configuration's crossSite names the callee, on
// another site its site has a gateway to. It gives each profile's units'
// access and the refusals, on the first instance of the unit's placement.
func CheckUnits(doc Document, sv Services) (map[string][]UnitAccess, []Refusal) {
	doc = doc.Normalized()
	profiles := map[string]Profile{}
	for _, p := range doc.Profiles {
		profiles[p.ID] = p
	}
	stacks := map[string]Stack{}
	for _, s := range doc.Stacks {
		stacks[s.ID] = s
	}
	access := map[string][]UnitAccess{}
	var refusals []Refusal
	type placed struct {
		site, profile, resource string
		access                  UnitAccess
	}
	var units []placed
	served := map[string]map[string]bool{} // by site
	bad := map[string]bool{}
	for _, pl := range doc.Placements {
		names := []string{pl.Profile}
		if pl.Stack != "" {
			names = stacks[pl.Stack].Profiles
		}
		for _, name := range names {
			prof, ok := profiles[name]
			if !ok {
				continue
			}
			for _, u := range prof.Units {
				if u.Kind != "serve" {
					continue
				}
				a, err := sv.Access(u)
				if err != nil {
					if !bad[name+"/"+u.Name] {
						bad[name+"/"+u.Name] = true
						refusals = append(refusals, Refusal{Resource: "profile:" + name, Reason: fmt.Sprintf("unit %s: %v", u.Name, err), Fix: "name services and groups serve --services takes"})
					}
					continue
				}
				if !slices.ContainsFunc(access[name], func(x UnitAccess) bool { return x.Unit == u.Name }) {
					access[name] = append(access[name], a)
				}
				for _, sc := range pl.Sites {
					if sc.Count == 0 {
						continue
					}
					if served[sc.Site] == nil {
						served[sc.Site] = map[string]bool{}
					}
					for _, s := range a.Services {
						served[sc.Site][s] = true
					}
					units = append(units, placed{site: sc.Site, profile: name, resource: InstanceID(sc.Site, pl.ID, name, 1), access: a})
				}
			}
		}
	}
	// Sites joined by a gateway, as the expansion makes them.
	joined := map[string]bool{}
	for _, g := range expandGateways(doc) {
		sites, _ := g.Props["sites"].([]any)
		if len(sites) == 2 {
			joined[fmt.Sprint(sites[0])+"~"+fmt.Sprint(sites[1])] = true
			joined[fmt.Sprint(sites[1])+"~"+fmt.Sprint(sites[0])] = true
		}
	}
	seen := map[string]bool{}
	for _, u := range units {
		for _, name := range u.access.Services {
			s, ok := sv.spec(name)
			if !ok {
				continue
			}
			for _, callee := range s.Manifest.Calls {
				if served[u.site][callee] {
					continue
				}
				reached := false
				if slices.Contains(doc.Settings.CrossSite, callee) {
					for other, set := range served {
						if other != u.site && set[callee] && joined[u.site+"~"+other] {
							reached = true
						}
					}
				}
				key := u.site + "/" + name + "/" + callee
				if reached || seen[key] {
					continue
				}
				seen[key] = true
				fix := fmt.Sprintf("add %s to a serve unit placed on %s", callee, u.site)
				if !slices.Contains(doc.Settings.CrossSite, callee) {
					fix += ", or allow it across sites in the settings' crossSite (with a gateway to a site that serves it)"
				}
				refusals = append(refusals, Refusal{Resource: u.resource, Reason: fmt.Sprintf("%s calls %s, which no unit serves on site %s", name, callee, u.site), Fix: fix})
			}
		}
	}
	return access, refusals
}
