package infra

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Step ops.
const (
	OpCreate = "create"
	OpChange = "change"
	OpRemove = "remove"
)

// Step is one thing a plan does to a resource, in run order; After are
// the steps it waits for.
type Step struct {
	ID        string         `json:"id"` // the resource's id: a plan has one step per resource
	Op        string         `json:"op"`
	Kind      string         `json:"kind"`
	Site      string         `json:"site,omitempty"`
	Account   string         `json:"account,omitempty"`
	Before    map[string]any `json:"before,omitempty"`
	Desired   map[string]any `json:"desired,omitempty"`
	After     []string       `json:"after,omitempty"`
	HoldsData bool           `json:"holdsData,omitempty"`
	Summary   string         `json:"summary"`
}

// Refusal is a resource a plan cannot satisfy, and why.
type Refusal struct {
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
	Fix      string `json:"fix,omitempty"`
}

// Actual is what exists: resources by the same ids, each account's sizes,
// and which hosts report they can run VMs.
type Actual struct {
	Resources []Resource                `json:"resources"`
	Sizes     map[string][]ProviderSize `json:"sizes,omitempty"`   // by account
	HostVMs   map[string]bool           `json:"hostVms,omitempty"` // by host resource id
}

// PlanResult is a plan's steps, refusals and notes.
type PlanResult struct {
	Steps    []Step    `json:"steps"`
	Refusals []Refusal `json:"refusals"`
	Notes    []string  `json:"notes"`
}

// Plan compares desired resources with actual ones: creations and changes
// in dependency order, removals first and in reverse, refusals for what
// cannot be satisfied (a resource needing VMs where nothing can run
// them), and notes. It is pure.
func Plan(desired []Resource, kinds map[string]ResourceKind, actual Actual) PlanResult {
	out := PlanResult{Steps: []Step{}, Refusals: []Refusal{}, Notes: []string{}}
	have := map[string]Resource{}
	for _, r := range actual.Resources {
		have[r.ID] = r
	}
	want := map[string]Resource{}
	for _, r := range desired {
		want[r.ID] = r
	}
	out.Refusals = capabilities(desired, actual)

	// Removals: what exists and is not desired, dependents first.
	var removes []Resource
	for _, r := range actual.Resources {
		if _, ok := want[r.ID]; !ok {
			removes = append(removes, r)
		}
	}
	sortResources(removes, kinds)
	for i, j := 0, len(removes)-1; i < j; i, j = i+1, j-1 {
		removes[i], removes[j] = removes[j], removes[i]
	}
	for _, r := range removes {
		order := kinds[r.Kind].Order
		var after []string
		for _, o := range removes {
			if o.Site == r.Site && kinds[o.Kind].Order > order {
				after = append(after, o.ID)
			}
		}
		out.Steps = append(out.Steps, Step{ID: r.ID, Op: OpRemove, Kind: r.Kind, Site: r.Site, Account: r.Account, Before: r.Props, After: after,
			HoldsData: r.HoldsData || kinds[r.Kind].HoldsData, Summary: "remove " + strings.ToLower(describeKind(kinds, r.Kind)) + " " + r.ID})
	}

	// Creations and changes, in dependency order.
	steps := map[string]Step{}
	var ids []string
	for _, r := range desired {
		if refused(out.Refusals, r.ID) {
			continue
		}
		cur, ok := have[r.ID]
		name := strings.ToLower(describeKind(kinds, r.Kind)) + " " + r.ID
		switch {
		case !ok:
			steps[r.ID] = Step{ID: r.ID, Op: OpCreate, Kind: r.Kind, Site: r.Site, Account: r.Account, Desired: r.Props, Summary: "create " + name}
		case differs(cur.Props, r.Props, kinds[r.Kind].Compared):
			steps[r.ID] = Step{ID: r.ID, Op: OpChange, Kind: r.Kind, Site: r.Site, Account: r.Account, Before: cur.Props, Desired: r.Props, Summary: "change " + name + changes(cur.Props, r.Props, kinds[r.Kind].Compared)}
		default:
			continue
		}
		ids = append(ids, r.ID)
	}
	for _, id := range ids {
		s := steps[id]
		for _, d := range want[id].DependsOn {
			if _, ok := steps[d]; ok {
				s.After = append(s.After, d)
			}
		}
		steps[id] = s
	}
	// Kahn's sort, ties by kind order then id (desired is already so sorted).
	done := map[string]bool{}
	for len(done) < len(ids) {
		progressed := false
		for _, id := range ids {
			if done[id] {
				continue
			}
			ready := true
			for _, a := range steps[id].After {
				if !done[a] {
					ready = false
					break
				}
			}
			if ready {
				out.Steps = append(out.Steps, steps[id])
				done[id] = true
				progressed = true
			}
		}
		if !progressed { // a cycle: list the rest as they are, refused
			for _, id := range ids {
				if !done[id] {
					out.Refusals = append(out.Refusals, Refusal{Resource: id, Reason: "it depends on itself through other resources"})
					done[id] = true
				}
			}
		}
	}

	// Notes: small clusters run NATS with fewer replicas.
	for _, r := range desired {
		if r.Kind != KindCluster {
			continue
		}
		n := 0
		for _, d := range r.DependsOn {
			if want[d].Kind == KindHost {
				n++
			}
		}
		if n > 0 && n < 3 {
			out.Notes = append(out.Notes, fmt.Sprintf("cluster %s has %d host(s): its NATS runs with %d replica(s)", r.Site, n, n))
		}
	}
	return out
}

func refused(rs []Refusal, id string) bool {
	for _, r := range rs {
		if r.Resource == id {
			return true
		}
	}
	return false
}

func sizeVMs(actual Actual, account, size string) (vms bool, known bool) {
	for _, s := range actual.Sizes[account] {
		if s.ID == size {
			return s.VMs, true
		}
	}
	return false, false
}

// capabilities refuses resources needing VMs where nothing can run them.
func capabilities(desired []Resource, actual Actual) []Refusal {
	out := []Refusal{}
	vmHosts := map[string]bool{} // site → a host there can run VMs
	for _, r := range desired {
		if r.Kind != KindHost {
			continue
		}
		size, _ := r.Props["size"].(string)
		vms, known := sizeVMs(actual, r.Account, size)
		if vms || actual.HostVMs[r.ID] {
			vmHosts[r.Site] = true
		}
		if r.NeedsVMs && !vms && !actual.HostVMs[r.ID] {
			reason := fmt.Sprintf("size %s cannot run VMs", size)
			if !known && r.Account != "" {
				reason = fmt.Sprintf("account %s offers no size %s", r.Account, size)
			}
			out = append(out, Refusal{Resource: r.ID, Reason: reason, Fix: "choose a size whose vms is true (size.list)"})
		}
	}
	for _, r := range desired {
		if r.Kind == KindInstance && r.NeedsVMs && !vmHosts[r.Site] {
			out = append(out, Refusal{Resource: r.ID, Reason: fmt.Sprintf("no host on %s can run VMs", r.Site), Fix: "add a host pool of a size that can run VMs to " + r.Site + ", or make the profile a container"})
		}
	}
	return out
}

func compared(a, b map[string]any, keys []string) []string {
	if len(keys) > 0 {
		return keys
	}
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func norm(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func differs(a, b map[string]any, keys []string) bool {
	for _, k := range compared(a, b, keys) {
		if !reflect.DeepEqual(norm(a[k]), norm(b[k])) {
			return true
		}
	}
	return false
}

func changes(a, b map[string]any, keys []string) string {
	var parts []string
	for _, k := range compared(a, b, keys) {
		if !reflect.DeepEqual(norm(a[k]), norm(b[k])) {
			parts = append(parts, fmt.Sprintf("%s %s → %s", k, short(a[k]), short(b[k])))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ", ")
}
