package infra

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// The states a described resource is in, from the plan of its version
// against what exists.
const (
	StatePresent = "present" // desired and made as desired
	StateMissing = "missing" // desired, not made yet
	StateChanged = "changed" // made, but differs from what is desired
	StateExtra   = "extra"   // made, but no longer desired
)

// Topology is a configuration version as resources by site, each with the
// state the plan against what exists gives it: what infra.describe gives
// the chat and the Topology page shows (forge 22p, 22q).
type Topology struct {
	Config   string         `json:"config"`
	Version  int            `json:"version"` // 0: the draft
	Sites    []SiteView     `json:"sites"`
	Refusals []Refusal      `json:"refusals"`
	Notes    []string       `json:"notes"`
	Counts   map[string]int `json:"counts"` // resources by state
}

// SiteView is one site of a topology with its resources in kind order.
type SiteView struct {
	Site
	Resources []ResourceState `json:"resources"`
}

// ResourceState is one resource and its state.
type ResourceState struct {
	Resource
	State   string `json:"state"`
	Summary string `json:"summary,omitempty"` // the plan step's, when it has one
}

// Describe expands a configuration version and plans it against what
// exists, without storing the plan. version < 0 is the applied version,
// else the head, else the draft; 0 is the draft.
func Describe(args features.ServiceArgs, reg *extensions.Registry, deps Deps, config string, version int) (Topology, error) {
	if config == "" {
		config = DefaultConfig
	}
	if deps.Store == nil {
		return Topology{}, fmt.Errorf("describing infrastructure is not available")
	}
	if version < 0 {
		version = 0
		if v, ok, err := deps.Store.Get(args.Context(), args.Principal.TenantID, ConfigProjector, ConfigType+"."+config); err == nil && ok {
			var c Config
			if json.Unmarshal(v.Value, &c) == nil {
				version = c.Applied
				if version == 0 {
					version = c.Head
				}
			}
		}
	}
	doc, err := LoadDocument(args, deps, config, version)
	if err != nil {
		return Topology{}, err
	}
	desired, kinds := Expand(reg, doc)
	act, err := ReadActual(args, deps, config, doc)
	if err != nil {
		return Topology{}, err
	}
	plan := Plan(desired, kinds, act)
	steps := map[string]Step{}
	for _, s := range plan.Steps {
		steps[s.ID] = s
	}
	order := map[string]int{}
	for _, k := range kinds {
		order[k.Kind] = k.Order
	}
	top := Topology{Config: config, Version: version, Refusals: plan.Refusals, Notes: plan.Notes, Counts: map[string]int{}}
	bySite := map[string]*SiteView{}
	norm := doc.Normalized()
	for _, s := range norm.Sites {
		top.Sites = append(top.Sites, SiteView{Site: s, Resources: []ResourceState{}})
	}
	for i := range top.Sites {
		bySite[top.Sites[i].ID] = &top.Sites[i]
	}
	add := func(r Resource, state, summary string) {
		top.Counts[state]++
		sv, ok := bySite[r.Site]
		if !ok {
			top.Sites = append(top.Sites, SiteView{Site: Site{ID: r.Site, Name: r.Site}, Resources: []ResourceState{}})
			for i := range top.Sites {
				bySite[top.Sites[i].ID] = &top.Sites[i]
			}
			sv = bySite[r.Site]
		}
		sv.Resources = append(sv.Resources, ResourceState{Resource: r, State: state, Summary: summary})
	}
	seen := map[string]bool{}
	for _, r := range desired {
		seen[r.ID] = true
		state := StatePresent
		s, ok := steps[r.ID]
		switch {
		case ok && s.Op == OpCreate:
			state = StateMissing
		case ok && s.Op == OpChange:
			state = StateChanged
		}
		add(r, state, s.Summary)
	}
	for _, r := range act.Resources {
		if !seen[r.ID] {
			add(r, StateExtra, steps[r.ID].Summary)
		}
	}
	for i := range top.Sites {
		rs := top.Sites[i].Resources
		sort.SliceStable(rs, func(a, b int) bool {
			if order[rs[a].Kind] != order[rs[b].Kind] {
				return order[rs[a].Kind] < order[rs[b].Kind]
			}
			return rs[a].ID < rs[b].ID
		})
	}
	if top.Refusals == nil {
		top.Refusals = []Refusal{}
	}
	if top.Notes == nil {
		top.Notes = []string{}
	}
	return top, nil
}

func contributeDescribe(reg *extensions.Registry, deps func() Deps) {
	RegisterToolAndCall(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "infra.describe", Source: source},
		Title:       "Describe infrastructure",
		Description: "The deployment as its configuration describes it, by site: each resource (host, cluster, instance, and later NATS nodes, units and stacks) with its state against what exists: present, missing (not made yet), changed (made, but differs) or extra (made, no longer desired), plus what cannot be satisfied and notes. version 0 is the draft; left out, the applied version, else the last committed one. Changes nothing.",
		Input:       json.RawMessage(`{"type":"object","properties":{"config":{"type":"string","description":"The configuration, main when left out"},"version":{"type":"integer","minimum":0,"description":"The version to describe; 0 for the draft; left out for the applied version"}}}`),
		Effect:      tools.EffectRead,
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Config  string
				Version *int
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			if ps := rootOnly(call.Principal); ps != nil {
				return tools.Result{}, fmt.Errorf("%s", ps[0].Message)
			}
			if err := checkRead(call.ServiceArgs); err != nil {
				return tools.Result{}, err
			}
			v := -1
			if a.Version != nil {
				v = *a.Version
			}
			top, err := Describe(call.ServiceArgs, reg, deps(), a.Config, v)
			if err != nil {
				return tools.Result{}, err
			}
			content := map[string]any{}
			raw, _ := json.Marshal(top)
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: fmt.Sprintf("%d site(s): %d present, %d missing, %d changed, %d extra",
				len(top.Sites), top.Counts[StatePresent], top.Counts[StateMissing], top.Counts[StateChanged], top.Counts[StateExtra])}, nil
		},
	})
}
