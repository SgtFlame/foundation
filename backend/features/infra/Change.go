package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ChangeOp is one edit of a draft: put an item of a kind, or remove one
// by id.
type ChangeOp struct {
	Op   string          `json:"op"` // put or remove
	Kind string          `json:"kind"`
	Item json.RawMessage `json:"item,omitempty"`
	ID   string          `json:"id,omitempty"`
}

// ChangeResult is what ChangeDraft did: the request its edits were made
// in (ops.undo undoes them together), the draft's changes since its head
// and the plan of the draft, unless asked for none.
type ChangeResult struct {
	Request string      `json:"request"`
	Changes []Change    `json:"changes"`
	Plan    *PlanRecord `json:"plan,omitempty"`
}

// ChangeDraft makes edits to a configuration's draft as one atomic batch
// of infra.config.put and infra.config.remove (forge 22q: what the chat
// proposes), then gives the draft's changes since its head and, with
// plan, the plan of the draft. It never commits or applies.
func ChangeDraft(args features.ServiceArgs, reg *extensions.Registry, deps Deps, config string, ops []ChangeOp, request, actor string, plan bool) (ChangeResult, error) {
	if config == "" {
		config = DefaultConfig
	}
	tx, ok := deps.Bus.(interface {
		Transact(bus.Request, []live.Op, bool) bus.TransactResult
	})
	if !ok || deps.Store == nil {
		return ChangeResult{}, fmt.Errorf("changing the configuration is not available")
	}
	if len(ops) == 0 {
		return ChangeResult{}, fmt.Errorf("there are no ops")
	}
	lops := make([]live.Op, 0, len(ops))
	for i, o := range ops {
		switch o.Op {
		case "put":
			if len(o.Item) == 0 {
				return ChangeResult{}, fmt.Errorf("ops.%d: a put needs its item", i)
			}
			lops = append(lops, live.OpOf("infra.config.put", config, map[string]any{"kind": o.Kind, "item": o.Item}))
		case "remove":
			lops = append(lops, live.OpOf("infra.config.remove", config, map[string]any{"kind": o.Kind, "id": o.ID}))
		default:
			return ChangeResult{}, fmt.Errorf("ops.%d: op is put or remove, not %q", i, o.Op)
		}
	}
	id := fmt.Sprintf("infra-change-%d", features.NewTSID())
	if request == "" {
		request = id
	}
	t := tx.Transact(bus.Request{ServiceArgs: args, ID: id, RequestID: request, Actor: actor}, lops, false)
	if t.Common.Code >= 300 {
		msg := t.Common.Message
		if len(t.Problems) > 0 {
			p := t.Problems[0]
			msg = p.Path + ": " + p.Message
			if p.Fix != "" {
				msg += " (" + p.Fix + ")"
			}
		}
		return ChangeResult{}, fmt.Errorf("the change was refused, nothing was changed: %s", msg)
	}
	if w, ok := deps.Store.(interface {
		WaitFor(ctx context.Context, tenant uint64, projector, key string, seq uint64, timeout time.Duration) (live.Projected, error)
	}); ok {
		_, _ = w.WaitFor(args.Context(), args.Principal.TenantID, ConfigProjector, ConfigType+"."+config, t.Seq, 5*time.Second)
	}
	out := ChangeResult{Request: t.RequestID, Changes: []Change{}}
	draft, err := LoadDocument(args, deps, config, 0)
	if err != nil {
		return out, err
	}
	var head Document
	if v, ok, _ := deps.Store.Get(args.Context(), args.Principal.TenantID, ConfigProjector, ConfigType+"."+config); ok {
		var c Config
		if json.Unmarshal(v.Value, &c) == nil && c.Head > 0 {
			if head, err = LoadDocument(args, deps, config, c.Head); err != nil {
				return out, err
			}
		}
	}
	out.Changes = Diff(head, draft)
	if plan {
		p, err := MakePlan(args, reg, deps, config, 0)
		if err != nil {
			return out, err
		}
		out.Plan = &p
	}
	return out, nil
}

func contributeChange(reg *extensions.Registry, deps func() Deps) {
	RegisterToolAndCall(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "infra.config.change", Source: source},
		Title:      "Change the infrastructure configuration",
		Description: "Makes several edits to the configuration's draft at once, all or none: each op puts an item of a kind (accounts, sites, clusters, hostPools, profiles, stacks, placements, overrides), replacing the item with its id, or removes one by id. Gives the draft's changes since its last committed version and the plan of the draft (what applying it would create, change and remove, and what cannot be satisfied). It never commits or applies: a person commits the draft and applies its plan. " +
			"For example, a third runner on site lab is a put of the runner placement with its count raised.",
		Input: json.RawMessage(`{"type":"object","properties":{
  "config":{"type":"string","description":"The configuration, main when left out"},
  "ops":{"type":"array","minItems":1,"items":{"type":"object","properties":{
    "op":{"type":"string","enum":["put","remove"],"description":"put an item, or remove one by id"},
    "kind":{"type":"string","enum":["` + strings.Join(Kinds, `","`) + `"],"description":"The list the item is in"},
    "item":{"type":"object","description":"For put: the whole item, with its id"},
    "id":{"type":"string","description":"For remove: the item's id"}},"required":["op","kind"]}},
  "plan":{"type":"boolean","description":"Plan the draft after the change (true when left out)"}},"required":["ops"]}`),
		Effect: tools.EffectWrite,
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Config string
				Ops    []ChangeOp
				Plan   *bool
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			if ps := rootOnly(call.Principal); ps != nil {
				return tools.Result{}, fmt.Errorf("%s", ps[0].Message)
			}
			plan := a.Plan == nil || *a.Plan
			r, err := ChangeDraft(call.ServiceArgs, reg, deps(), a.Config, a.Ops, livetools.RequestOf(call), livetools.ActorOf(call), plan)
			if err != nil {
				return tools.Result{}, err
			}
			content := map[string]any{}
			raw, _ := json.Marshal(r)
			_ = json.Unmarshal(raw, &content)
			summary := fmt.Sprintf("%d change(s) to the draft since its head", len(r.Changes))
			if r.Plan != nil {
				summary += fmt.Sprintf("; plan %s: %d step(s), %d refused", r.Plan.ID, len(r.Plan.Steps), len(r.Plan.Refusals))
			}
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})
}
