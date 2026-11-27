package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Plans live on the infra area (state, not definitions), one target each.
const (
	PlanType      = "infraPlan"
	PlanProjector = "infra.plan"
	EventPlanned  = "planned"
)

// Plan statuses.
const (
	PlanReady    = "ready"    // no refusals: it may be applied
	PlanRefused  = "refused"  // it has refusals: it cannot be applied
	PlanApplying = "applying" // 22f
	PlanApplied  = "applied"
	PlanFailed   = "failed"
	PlanStale    = "stale"
)

// PlanRecord is a stored plan: what a version would do to the actual
// state read at ActualAt.
type PlanRecord struct {
	ID        string    `json:"id"`
	Config    string    `json:"config"`
	Version   int       `json:"version"`
	Status    string    `json:"status"`
	ActualAt  time.Time `json:"actualAt"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	PlanResult
}

// Compute calls an infra.compute operation through a provider account's
// connection.
type Compute interface {
	Call(args features.ServiceArgs, account Account, op string, in any) (json.RawMessage, error)
}

// DispatcherCompute calls infra.compute over the connector bus.
type DispatcherCompute struct{ D *connectors.Dispatcher }

// Call sends one infra.compute call on the account's connection.
func (c DispatcherCompute) Call(args features.ServiceArgs, account Account, op string, in any) (json.RawMessage, error) {
	raw, _ := json.Marshal(in)
	out, err := c.D.Call(args, connectors.Call{Contract: ComputeRef, Operation: op, Connection: account.Connection, Args: raw})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Executor sends commands: the live bus.
type Executor interface {
	Execute(bus.Request) bus.Result
}

// Deps are what infra needs at run time, made after the registry.
type Deps struct {
	Bus     Executor
	Store   live.Store
	Compute Compute
	// Actual gives the actual resources Compute does not (clusters and
	// instances the Control Plane reports); nil for none.
	Actual func(args features.ServiceArgs, config string) ([]Resource, error)
}

// ReadActual gathers what exists for a document: each account's hosts
// (host.list, keyed back to resource ids by their provider names) and
// sizes, and what Deps.Actual adds.
func ReadActual(args features.ServiceArgs, deps Deps, config string, docs ...Document) (Actual, error) {
	act := Actual{Resources: []Resource{}, Sizes: map[string][]ProviderSize{}, HostVMs: map[string]bool{}}
	accounts := map[string]Account{}
	byName := map[string]Resource{} // provider name → desired host
	for _, d := range docs {
		for _, a := range d.Accounts {
			accounts[a.ID] = a
		}
		for _, r := range expandHosts(d.Normalized()) {
			byName[r.Account+"/"+r.Props["name"].(string)] = r
		}
	}
	if deps.Compute != nil {
		for _, id := range sortedKeys(accounts) {
			a := accounts[id]
			if a.Connection == "" {
				continue
			}
			var sizes struct{ Sizes []ProviderSize }
			raw, err := deps.Compute.Call(args, a, "size.list", map[string]any{})
			if err != nil {
				return Actual{}, fmt.Errorf("account %s: size.list: %w", a.ID, err)
			}
			_ = json.Unmarshal(raw, &sizes)
			act.Sizes[a.ID] = sizes.Sizes
			var hosts struct{ Hosts []ProviderHost }
			if raw, err = deps.Compute.Call(args, a, "host.list", map[string]any{}); err != nil {
				return Actual{}, fmt.Errorf("account %s: host.list: %w", a.ID, err)
			}
			_ = json.Unmarshal(raw, &hosts)
			for _, h := range hosts.Hosts {
				if h.State == HostGone {
					continue
				}
				r := Resource{ID: "host:" + a.ID + "/" + h.ID, Kind: KindHost, Account: a.ID,
					Props: map[string]any{"name": h.ID, "size": h.Size, "region": h.Region, "providerId": h.ProviderID, "state": h.State}}
				if h.Image != "" {
					r.Props["image"] = h.Image
				}
				if d, ok := byName[a.ID+"/"+h.ID]; ok {
					r.ID, r.Site = d.ID, d.Site
					r.Props["pool"] = d.Props["pool"]
					if _, ok := d.Props["image"]; !ok {
						delete(r.Props, "image") // the provider's default
					}
				}
				act.Resources = append(act.Resources, r)
				if h.Nested {
					act.HostVMs[r.ID] = true
				}
			}
		}
	}
	if deps.Actual != nil {
		more, err := deps.Actual(args, config)
		if err != nil {
			return Actual{}, err
		}
		act.Resources = append(act.Resources, more...)
	}
	return act, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// MakePlan plans a version (0: the draft) against the actual state and
// stores the plan, returning it.
func MakePlan(args features.ServiceArgs, reg *extensions.Registry, deps Deps, config string, version int) (PlanRecord, error) {
	if config == "" {
		config = DefaultConfig
	}
	if deps.Store == nil || deps.Bus == nil {
		return PlanRecord{}, fmt.Errorf("planning is not available")
	}
	tenant := args.Principal.TenantID
	if s, ok := deps.Store.(interface {
		Settle(ctx context.Context, tenant uint64, projector string) error
	}); ok && version == 0 {
		_ = s.Settle(args.Context(), tenant, ConfigProjector) // read the draft as last committed
	}
	var doc Document
	if version == 0 {
		v, ok, err := deps.Store.Get(args.Context(), tenant, ConfigProjector, ConfigType+"."+config)
		if err != nil {
			return PlanRecord{}, err
		}
		if !ok {
			return PlanRecord{}, fmt.Errorf("there is no configuration %s", config)
		}
		var c Config
		if err := json.Unmarshal(v.Value, &c); err != nil {
			return PlanRecord{}, err
		}
		doc = c.Draft
	} else {
		v, ok, err := deps.Store.Get(args.Context(), tenant, VersionProjector, VersionType+"."+VersionID(config, version))
		if err != nil {
			return PlanRecord{}, err
		}
		if !ok {
			return PlanRecord{}, fmt.Errorf("there is no version %d", version)
		}
		var ver Version
		if err := json.Unmarshal(v.Value, &ver); err != nil {
			return PlanRecord{}, err
		}
		doc = ver.Document
	}
	desired, kinds := Expand(reg, doc)
	act, err := ReadActual(args, deps, config, doc)
	if err != nil {
		return PlanRecord{}, err
	}
	rec := PlanRecord{Config: config, Version: version, ActualAt: time.Now().UTC(), PlanResult: Plan(desired, kinds, act)}
	raw, _ := json.Marshal(map[string]any{"plan": rec})
	id := strconv.FormatUint(features.NewTSID(), 10)
	r := deps.Bus.Execute(bus.Request{ServiceArgs: args, ID: "plan-" + id, Type: "infra.plan.record", Target: id, Args: raw})
	if r.Common.Code >= 300 {
		return PlanRecord{}, fmt.Errorf("%s", r.Common.Message)
	}
	rec.ID = id
	return rec, nil
}

type planEvent struct {
	Plan   *PlanRecord `json:"plan,omitempty"`
	Status string      `json:"status,omitempty"`
}

func foldPlan(f live.Fold) (json.RawMessage, error) {
	var p PlanRecord
	if f.State != nil {
		if err := json.Unmarshal(f.State, &p); err != nil {
			return nil, err
		}
	}
	var e planEvent
	if err := json.Unmarshal(f.Event.Data, &e); err != nil {
		return nil, err
	}
	switch f.Event.Type {
	case EventPlanned:
		if e.Plan != nil {
			p = *e.Plan
			p.ID = f.Event.Target.ID
			p.CreatedAt = f.Event.Time
		}
	}
	return json.Marshal(p)
}

const planFields = "{id, config, version (0: the draft), status (ready, refused, applying, applied, failed, stale), actualAt, createdBy, createdAt, steps: [{id (the resource id), op: create | change | remove, kind, site, account, before, desired, after (step ids it waits for), holdsData, summary}], refusals: [{resource, reason, fix}], notes}"

func contributePlans(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: PlanProjector, Source: source},
		Area:       Area, Types: []string{PlanType}, Fold: foldPlan,
	})
	live.Commands.Register(reg, live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.plan.record", Source: source}, Title: "Record a plan",
		Description: "Stores a plan infra.plan made (internal).",
		Area:        Area, TargetType: PlanType, Projection: PlanProjector, Action: ActionOperate, Internal: true,
		Args:     json.RawMessage(`{"type":"object","properties":{"plan":{"type":"object","description":"The plan: ` + planFields + `"}},"required":["plan"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"plan":{"config":"main","version":1,"steps":[],"refusals":[],"notes":[]}}`)},
		Events:   []string{EventPlanned},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			if ps := rootOnly(d.Principal); ps != nil {
				return nil, ps
			}
			if d.State != nil {
				return nil, problem("", "", "the plan exists", "")
			}
			var a struct {
				Plan PlanRecord `json:"plan"`
			}
			if err := json.Unmarshal(d.Args, &a); err != nil {
				return nil, problem("", "args.plan", err.Error(), "")
			}
			p := a.Plan
			p.ID, p.CreatedBy = d.Target.ID, authorOf(d.Principal)
			p.Status = PlanReady
			if len(p.Refusals) > 0 {
				p.Status = PlanRefused
			}
			return []live.NewEvent{{Type: EventPlanned, Data: planEvent{Plan: &p}}}, nil
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.plans", Source: source}, Title: "Infrastructure plans",
		Description: "Every stored plan, keyed infraPlan.<id>: " + planFields,
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      ActionRead,
		Run:         live.ListOf(PlanProjector, func(f live.Feed) (string, error) { return PlanType + ".*", nil }),
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.planOf", Source: source}, Title: "An infrastructure plan",
		Description: "One stored plan, or null: " + planFields,
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The plan's id"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"123"}`)},
		Action:      ActionRead,
		Run: live.ValueOf(PlanProjector, func(f live.Feed) (string, error) {
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.ID) {
				return "", fmt.Errorf("infra.planOf needs a plan's id")
			}
			return PlanType + "." + p.ID, nil
		}, json.RawMessage(`null`)),
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "infra.plan", Source: source},
		Title:       "Plan infrastructure",
		Description: "Compares a configuration version (0 or left out: the draft) with what exists at the providers and stores a plan: the creations, changes and removals in the order they would run, refusals of what cannot be satisfied (such as a VM where no host can run VMs) and notes. Applying is always of a plan a person has seen. Gives the plan with its id.",
		Input:       json.RawMessage(`{"type":"object","properties":{"config":{"type":"string","description":"The configuration, main when left out"},"version":{"type":"integer","minimum":0,"description":"The version to plan; 0 or left out for the draft"}}}`),
		Effect:      tools.EffectWrite,
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Config  string
				Version int
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			if ps := rootOnly(call.Principal); ps != nil {
				return tools.Result{}, fmt.Errorf("%s", ps[0].Message)
			}
			rec, err := MakePlan(call.ServiceArgs, reg, deps(), a.Config, a.Version)
			if err != nil {
				return tools.Result{}, err
			}
			content := map[string]any{}
			raw, _ := json.Marshal(rec)
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: fmt.Sprintf("Planned %d step(s), %d refused", len(rec.Steps), len(rec.Refusals))}, nil
		},
	})
}
