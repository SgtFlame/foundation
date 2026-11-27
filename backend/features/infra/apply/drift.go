package infraapply

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Drift is what CheckDrift found: the plan of the applied version against
// what exists, and the apply that corrects it when onDrift is correct.
type Drift struct {
	Version int              `json:"version"`
	Plan    infra.PlanRecord `json:"plan"`
	Applied string           `json:"applied,omitempty"` // the correcting apply's id
}

// CheckDrift plans a configuration's applied version against what exists;
// every step is drift. When the configuration's onDrift is correct and the
// plan has no refusals, it applies the plan (removals of what holds data
// still wait for approval).
func CheckDrift(args features.ServiceArgs, reg *extensions.Registry, d Deps, config string) (Drift, error) {
	if config == "" {
		config = infra.DefaultConfig
	}
	settleStore(args, d.Infra.Store, infra.ConfigProjector)
	v, ok, err := d.Infra.Store.Get(args.Context(), args.Principal.TenantID, infra.ConfigProjector, infra.ConfigType+"."+config)
	if err != nil {
		return Drift{}, err
	}
	var c infra.Config
	if ok {
		_ = json.Unmarshal(v.Value, &c)
	}
	if c.Applied == 0 {
		return Drift{}, fmt.Errorf("configuration %s has no applied version", config)
	}
	p, err := infra.MakePlan(args, reg, d.Infra, config, c.Applied)
	if err != nil {
		return Drift{}, err
	}
	out := Drift{Version: c.Applied, Plan: p}
	if len(p.Steps) > 0 && len(p.Refusals) == 0 && c.Draft.Settings.OnDrift == "correct" {
		raw, _ := json.Marshal(map[string]any{"plan": p.ID})
		r := d.Infra.Bus.Execute(bus.Request{ServiceArgs: args, ID: "drift-" + p.ID, Type: "infra.apply", Args: raw, Actor: "infra"})
		if r.Common.Code >= 300 {
			return out, fmt.Errorf("%s", r.Common.Message)
		}
		out.Applied = p.ID
	}
	return out, nil
}

func contributeDrift(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "infra.drift", Source: source},
		Title:       "Check infrastructure drift",
		Description: "Plans the configuration's applied version against what exists at the providers and in the Control Plane: every step is drift (a host that vanished, an instance changed by hand). When the configuration's onDrift setting is correct, applies that plan at once (removals of what holds data still wait for approval). Gives the plan and the correcting apply's id.",
		Input:       json.RawMessage(`{"type":"object","properties":{"config":{"type":"string","description":"The configuration, main when left out"}}}`),
		Effect:      tools.EffectWrite,
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Config string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			if ps := rootOnly(call.Principal); ps != nil {
				return tools.Result{}, fmt.Errorf("%s", ps[0].Message)
			}
			dr, err := CheckDrift(call.ServiceArgs, reg, deps(), a.Config)
			if err != nil {
				return tools.Result{}, err
			}
			content := map[string]any{}
			raw, _ := json.Marshal(dr)
			_ = json.Unmarshal(raw, &content)
			summary := fmt.Sprintf("No drift from version %d", dr.Version)
			if n := len(dr.Plan.Steps); n > 0 {
				summary = fmt.Sprintf("%d step(s) of drift from version %d", n, dr.Version)
				if dr.Applied != "" {
					summary += ", being corrected"
				}
			}
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})
}
