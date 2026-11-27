// Package infraapply applies infrastructure plans (forge 22f): an apply
// walks a plan's steps as durable jobs in dependency order, each step's
// job enqueueing the steps it unblocks in the same commit that records
// it, so there is no leader and a repeat enqueues nothing new. Removals
// of what holds data wait for an approval. Drift is a plan of the applied
// version against what exists; rollback is applying an earlier version.
package infraapply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	infranodes "github.com/SgtFlame/foundation/backend/features/infra/nodes"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
)

const source = "infra"

// Applies live on the infra area, one target per plan (an apply's id is
// its plan's).
const (
	ApplyType      = "infraApply"
	ApplyProjector = "infra.apply"
	StepGraph      = "infra-apply-step"
	Queue          = "infra"
)

// PlanMaxAge is how old a plan may be when it is applied (22d question 2;
// Tony to confirm).
var PlanMaxAge = 15 * time.Minute

// Step and apply statuses.
const (
	StepWaiting  = "waiting"  // a step it waits for is not done
	StepReady    = "ready"    // its job is enqueued
	StepProposed = "proposed" // it removes what holds data: it waits for an approval
	StepDone     = "done"
	StepFailed   = "failed"

	ApplyRunning = "running"
	ApplyDone    = "done"
	ApplyFailed  = "failed"  // a step failed: Resume runs it again
	ApplyWaiting = "waiting" // only proposals are left
)

// StepState is a plan step as an apply runs it.
type StepState struct {
	infra.Step
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Attempt int    `json:"attempt"` // raised by each resume of the step
}

// Apply is an apply of a plan.
type Apply struct {
	ID        string      `json:"id"` // the plan's id
	Config    string      `json:"config"`
	Version   int         `json:"version"`
	Status    string      `json:"status"`
	Steps     []StepState `json:"steps"`
	CreatedBy string      `json:"createdBy"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// Step finds a step.
func (a *Apply) Step(id string) *StepState {
	for i := range a.Steps {
		if a.Steps[i].ID == id {
			return &a.Steps[i]
		}
	}
	return nil
}

// Deps are an apply's run-time dependencies.
type Deps struct {
	Infra infra.Deps
	Nodes infranodes.Deps
	// Outbox makes sure the infra area's job outbox is followed
	// (jobs.Runner.Outbox), so the step jobs a commit names are enqueued.
	Outbox func(args features.ServiceArgs, area live.Area) error
}

// JobID is a step's job id: a repeat of the same attempt is dropped.
func JobID(apply string, s StepState) string {
	return fmt.Sprintf("apply-%s-%s-%d", apply, infranodes.RecordID(s.ID), s.Attempt)
}

// advance makes ready (and enqueues) the waiting steps whose steps are
// all done, proposes those that remove what holds data, and sets the
// apply's status.
func advance(d live.Decision, a *Apply) {
	done := map[string]bool{}
	for _, s := range a.Steps {
		if s.Status == StepDone {
			done[s.ID] = true
		}
	}
	for i := range a.Steps {
		s := &a.Steps[i]
		if s.Status != StepWaiting {
			continue
		}
		ready := true
		for _, dep := range s.After {
			if !done[dep] {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		if s.Op == infra.OpRemove && s.HoldsData {
			s.Status = StepProposed
			continue
		}
		enqueue(d, a, s)
	}
	a.Status = status(a)
}

func enqueue(d live.Decision, a *Apply, s *StepState) {
	s.Status = StepReady
	inputs, _ := json.Marshal(map[string]any{"apply": a.ID, "step": s.ID})
	j := live.Job{ID: JobID(a.ID, *s), Graph: StepGraph, Inputs: inputs, Queue: Queue, As: d.Principal.Ref()}
	if s.Site != "" {
		j.Key = "infra-site-" + s.Site // one site's provider is not flooded (22f question 2)
	}
	d.Enqueue(j)
}

func status(a *Apply) string {
	n := map[string]int{}
	for _, s := range a.Steps {
		n[s.Status]++
	}
	switch {
	case n[StepDone] == len(a.Steps):
		return ApplyDone
	case n[StepFailed] > 0 && n[StepReady] == 0:
		return ApplyFailed
	case n[StepReady] == 0 && n[StepProposed] > 0:
		return ApplyWaiting
	}
	return ApplyRunning
}

type applyEvent struct {
	Apply Apply `json:"apply"`
}

func foldApply(f live.Fold) (json.RawMessage, error) {
	var e applyEvent
	if err := json.Unmarshal(f.Event.Data, &e); err != nil {
		return nil, err
	}
	e.Apply.ID = f.Event.Target.ID
	e.Apply.UpdatedAt = f.Event.Time
	return json.Marshal(e.Apply)
}

func problem(code, path, msg, fix string) []live.Problem {
	return []live.Problem{{Code: code, Path: path, Message: msg, Fix: fix}}
}

func rootOnly(p features.Principal) []live.Problem {
	if p.TenantID != features.RootTenantID {
		return problem("not_root_tenant", "", "infrastructure lives only in the root tenant", "")
	}
	return nil
}

const applyFields = "{id (the plan's), config, version, status (running, done, failed, waiting), createdBy, updatedAt, steps: [{id, op, kind, site, after, holdsData, summary, status (waiting, ready, proposed, done, failed), message, attempt}]}"

func readApply(d live.Decision) (*Apply, []live.Problem) {
	if d.State == nil {
		return nil, problem("apply_not_found", "", fmt.Sprintf("there is no apply %s", d.Target.ID), "list them with infra.applies")
	}
	var a Apply
	if err := json.Unmarshal(d.State, &a); err != nil {
		return nil, problem("", "", "the apply cannot be read: "+err.Error(), "")
	}
	return &a, nil
}

func register(reg *extensions.Registry, deps func() Deps, c live.Command, decide func(d live.Decision) ([]live.NewEvent, []live.Problem)) {
	c.Source, c.Area, c.TargetType, c.Projection = source, infra.Area, ApplyType, ApplyProjector
	c.Decide = func(d live.Decision) ([]live.NewEvent, []live.Problem) {
		if ps := rootOnly(d.Principal); ps != nil {
			return nil, ps
		}
		// The jobs a decision enqueues are enqueued by the area's outbox
		// follower, which must be followed before the commit.
		if out := deps().Outbox; out != nil {
			if err := out(d.ServiceArgs, infra.Area); err != nil {
				return nil, []live.Problem{{Message: "the jobs of infrastructure applies cannot be enqueued: " + err.Error()}}
			}
		}
		return decide(d)
	}
	live.Commands.Register(reg, c)
}

func emit(typ string, a *Apply) []live.NewEvent {
	return []live.NewEvent{{Type: typ, Data: applyEvent{Apply: *a}}}
}

// Contribute registers applies: the projector, commands, queries, the
// step node and its built-in graph, and the drift tool.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	contributeTopology(reg)
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: ApplyProjector, Source: source},
		Area:       infra.Area, Types: []string{ApplyType}, Fold: foldApply,
	})

	register(reg, deps, live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.apply"}, Title: "Apply a plan",
		Description: "Applies a plan infra.plan made: its steps run as durable jobs in dependency order, each enqueueing the steps it unblocks; a step that removes what holds data waits as a proposal for infra.apply.approve. Refused for a plan with refusals (plan_refused), one older than 15 minutes (plan_stale: plan again) or one already applied (use infra.apply.resume). Follow it with infra.applyOf {id: plan}.",
		Action:      infra.ActionAdmin,
		Args:        json.RawMessage(`{"type":"object","properties":{"plan":{"type":"string","description":"The plan's id, from infra.plan or infra.plans"}},"required":["plan"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"plan":"119661739459902763"}`)},
		Events:      []string{"started"},
		NewTargetFor: func(_ features.ServiceArgs, raw json.RawMessage) string {
			var a struct{ Plan string }
			_ = json.Unmarshal(raw, &a)
			return a.Plan
		},
	}, func(d live.Decision) ([]live.NewEvent, []live.Problem) {
		var in struct{ Plan string }
		_ = json.Unmarshal(d.Args, &in)
		if in.Plan != d.Target.ID {
			return nil, problem("", "args.plan", "the target is the plan's id", "")
		}
		if d.State != nil {
			return nil, problem("already_applied", "args.plan", "this plan was applied already", "resume it with infra.apply.resume, or plan again")
		}
		raw, err := d.Read(infra.PlanProjector, live.Target{Area: infra.Area, Type: infra.PlanType, ID: in.Plan})
		if err != nil || raw == nil {
			return nil, problem("plan_not_found", "args.plan", fmt.Sprintf("there is no plan %s", in.Plan), "make one with infra.plan")
		}
		var p infra.PlanRecord
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, problem("", "args.plan", err.Error(), "")
		}
		if len(p.Refusals) > 0 {
			return nil, problem("plan_refused", "args.plan", fmt.Sprintf("the plan has %d refusal(s): %s", len(p.Refusals), p.Refusals[0].Reason), "change the configuration and plan again")
		}
		if time.Since(p.ActualAt) > PlanMaxAge {
			return nil, problem("plan_stale", "args.plan", "the plan is older than "+PlanMaxAge.String(), "plan again with infra.plan")
		}
		a := &Apply{ID: d.Target.ID, Config: p.Config, Version: p.Version, CreatedBy: p.CreatedBy, Steps: make([]StepState, 0, len(p.Steps))}
		if d.Principal.UserID != 0 {
			a.CreatedBy = fmt.Sprint(d.Principal.UserID)
		}
		for _, s := range p.Steps {
			a.Steps = append(a.Steps, StepState{Step: s, Status: StepWaiting})
		}
		advance(d, a)
		return emit("started", a), nil
	})

	register(reg, deps, live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.apply.step"}, Title: "Record an apply step",
		Description: "Records a step's outcome (sent by its job) and enqueues the steps it unblocks.",
		Action:      infra.ActionAdmin, Internal: true,
		Args: json.RawMessage(`{"type":"object","properties":{
  "step":{"type":"string","description":"The step's id"},
  "attempt":{"type":"integer","minimum":0,"description":"The attempt the job ran"},
  "ok":{"type":"boolean","description":"Whether it succeeded"},
  "message":{"type":"string","description":"What happened"}},"required":["step","ok"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"step":"host:west/general/1","ok":true}`)},
		Events:   []string{"stepDone", "stepFailed"},
	}, func(d live.Decision) ([]live.NewEvent, []live.Problem) {
		a, ps := readApply(d)
		if ps != nil {
			return nil, ps
		}
		var in struct {
			Step    string
			Attempt int
			OK      bool
			Message string
		}
		_ = json.Unmarshal(d.Args, &in)
		s := a.Step(in.Step)
		if s == nil {
			return nil, problem("", "args.step", fmt.Sprintf("the apply has no step %s", in.Step), "")
		}
		if s.Status != StepReady || s.Attempt != in.Attempt {
			return nil, nil // recorded already, or an earlier attempt's late answer
		}
		s.Message = in.Message
		typ := "stepDone"
		if in.OK {
			s.Status = StepDone
		} else {
			s.Status, typ = StepFailed, "stepFailed"
		}
		advance(d, a)
		return emit(typ, a), nil
	})

	register(reg, deps, live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.apply.approve"}, Title: "Approve a removal",
		Description: "Approves a proposed step of an apply, one that removes what holds data (an instance, a cluster), so it runs. The target is the apply (its plan's id).",
		Action:      infra.ActionApprove,
		Args:        json.RawMessage(`{"type":"object","properties":{"step":{"type":"string","description":"The proposed step's id, e.g. instance:west/web/web/2"}},"required":["step"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"step":"instance:west/web/web/2"}`)},
		Events:      []string{"approved"},
	}, func(d live.Decision) ([]live.NewEvent, []live.Problem) {
		a, ps := readApply(d)
		if ps != nil {
			return nil, ps
		}
		var in struct{ Step string }
		_ = json.Unmarshal(d.Args, &in)
		s := a.Step(in.Step)
		if s == nil || s.Status != StepProposed {
			return nil, problem("not_proposed", "args.step", fmt.Sprintf("step %s is not waiting for an approval", in.Step), "")
		}
		enqueue(d, a, s)
		a.Status = status(a)
		return emit("approved", a), nil
	})

	register(reg, deps, live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.apply.resume"}, Title: "Resume an apply",
		Description: "Runs an apply's failed steps again, and those whose jobs ran out of attempts, each as a new attempt; the steps waiting for them follow. The target is the apply (its plan's id).",
		Action:      infra.ActionAdmin,
		Args:        json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Events:      []string{"resumed"},
	}, func(d live.Decision) ([]live.NewEvent, []live.Problem) {
		a, ps := readApply(d)
		if ps != nil {
			return nil, ps
		}
		n := 0
		for i := range a.Steps {
			s := &a.Steps[i]
			if s.Status == StepFailed || s.Status == StepReady {
				s.Attempt++
				s.Message = ""
				enqueue(d, a, s)
				n++
			}
		}
		if n == 0 {
			return nil, problem("nothing_to_resume", "", "no step failed or is still running", "")
		}
		a.Status = status(a)
		return emit("resumed", a), nil
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.applies", Source: source}, Title: "Infrastructure applies",
		Description: "Every apply, keyed infraApply.<plan id>: " + applyFields,
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      infra.ActionRead,
		Run:         live.ListOf(ApplyProjector, func(f live.Feed) (string, error) { return ApplyType + ".*", nil }),
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.applyOf", Source: source}, Title: "An infrastructure apply",
		Description: "One apply, or null: " + applyFields,
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The apply's id: its plan's"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"119661739459902763"}`)},
		Action:      infra.ActionRead,
		Run: live.ValueOf(ApplyProjector, func(f live.Feed) (string, error) {
			var p struct{ ID string }
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.ID) {
				return "", fmt.Errorf("infra.applyOf needs an apply's id")
			}
			return ApplyType + "." + p.ID, nil
		}, json.RawMessage(`null`)),
	})

	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.applyStep", Source: source, Order: 1100}, Title: "Run apply step", Category: "Infrastructure",
		Description: "Runs one step of an infrastructure apply (the infra-apply-step job's work): ensures or removes the step's resource with the infrastructure nodes' logic, then records the outcome, which enqueues the steps it unblocks. A provider error that may pass (rateLimited, unavailable, conflict) fails the run so the job tries again; any other failure is recorded on the step for Resume.",
		Example:     "Run apply step with the apply and step the job was given",
		Tags:        []string{"infrastructure", "apply", "jobs"},
		Kinds:       []string{livenodes.KindAction}, External: true, Effects: true,
		Sample: cognition.Values{"status": "done"},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("apply", "Apply", "string", "The apply's id"),
				cognition.DataPin("step", "Step", "string", "The step's id")},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "Then"),
				cognition.DataPin("status", "Status", "string", "The step's status after it: done or failed")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			apply, _ := cognition.Get[string](in, "apply")
			step, _ := cognition.Get[string](in, "step")
			st, err := RunStep(x.ServiceArgs, deps(), apply, step)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"status": st}, cognition.Then, nil
		},
	})
	contributeGraph(reg)
	contributeDrift(reg, deps)
}

func contributeGraph(reg *extensions.Registry) {
	g := cognition.Graph{}
	raw := `{"id":"` + StepGraph + `","name":"Infrastructure apply step","kind":"` + livenodes.KindAction + `",
"description":"Runs one step of an infrastructure apply: the job each step of infra.apply enqueues.",
"nodes":[
 {"id":"event","type":"` + cognition.EventCall + `","position":{"x":0,"y":0},"config":{"outputs":[{"id":"apply","type":"string"},{"id":"step","type":"string"}]}},
 {"id":"run","type":"infra.applyStep","position":{"x":300,"y":0}}],
"edges":[
 {"from":{"node":"event","pin":"then"},"to":{"node":"run","pin":"then"}},
 {"from":{"node":"event","pin":"apply"},"to":{"node":"run","pin":"apply"}},
 {"from":{"node":"event","pin":"step"},"to":{"node":"run","pin":"step"}}]}`
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		panic(err)
	}
	cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: StepGraph, Order: 700, Source: source}, Graph: g, Editable: true})
}

// retryable reports an error a job should retry.
func retryable(err error) bool {
	var r interface{ Retryable() bool }
	return errors.As(err, &r) && r.Retryable()
}

func settleStore(args features.ServiceArgs, store live.Store, projector string) {
	if s, ok := store.(interface {
		Settle(ctx context.Context, tenant uint64, projector string) error
	}); ok {
		_ = s.Settle(args.Context(), args.Principal.TenantID, projector)
	}
}

// RunStep runs one ready step of an apply and records its outcome; a step
// not ready (recorded already, or not yet) does nothing. It returns the
// step's status, or a retryable provider error for the job to retry.
func RunStep(args features.ServiceArgs, d Deps, applyID, stepID string) (string, error) {
	store := d.Infra.Store
	if store == nil || d.Infra.Bus == nil {
		return "", fmt.Errorf("applying is not available")
	}
	settleStore(args, store, ApplyProjector)
	v, ok, err := store.Get(args.Context(), args.Principal.TenantID, ApplyProjector, ApplyType+"."+applyID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("there is no apply %s", applyID)
	}
	var a Apply
	if err := json.Unmarshal(v.Value, &a); err != nil {
		return "", err
	}
	s := a.Step(stepID)
	if s == nil {
		return "", fmt.Errorf("the apply has no step %s", stepID)
	}
	if s.Status != StepReady {
		return s.Status, nil
	}
	pv, _, err := store.Get(args.Context(), args.Principal.TenantID, infra.PlanProjector, infra.PlanType+"."+applyID)
	if err != nil {
		return "", err
	}
	var plan infra.PlanRecord
	_ = json.Unmarshal(pv.Value, &plan)
	account := infra.Account{ID: s.Account}
	for _, acc := range plan.Accounts {
		if acc.ID == s.Account {
			account = acc
		}
	}
	msg, runErr := runStep(args, d.Nodes, s.Step, account)
	if runErr != nil && retryable(runErr) {
		return "", runErr
	}
	ok = runErr == nil
	if runErr != nil {
		msg = runErr.Error()
	}
	raw, _ := json.Marshal(map[string]any{"step": stepID, "attempt": s.Attempt, "ok": ok, "message": msg})
	r := d.Infra.Bus.Execute(bus.Request{ServiceArgs: args, ID: "applied-" + JobID(applyID, *s), Type: "infra.apply.step", Target: applyID, Args: raw, Actor: "infra"})
	if r.Common.Code >= 300 {
		return "", fmt.Errorf("%s", r.Common.Message)
	}
	status := StepDone
	if !ok {
		status = StepFailed
	}
	// The last step marks the configuration applied.
	if a.Version > 0 {
		settleStore(args, store, ApplyProjector)
		if v, ok, _ := store.Get(args.Context(), args.Principal.TenantID, ApplyProjector, ApplyType+"."+applyID); ok {
			var now Apply
			if json.Unmarshal(v.Value, &now) == nil && now.Status == ApplyDone {
				raw, _ := json.Marshal(map[string]any{"number": a.Version})
				d.Infra.Bus.Execute(bus.Request{ServiceArgs: args, ID: "applied-" + applyID, Type: "infra.config.markApplied", Target: a.Config, Args: raw, Actor: "infra"})
			}
		}
	}
	return status, nil
}

// runStep does a step's work, giving what it did.
func runStep(args features.ServiceArgs, d infranodes.Deps, s infra.Step, account infra.Account) (string, error) {
	if s.Op == infra.OpRemove {
		removed, err := infranodes.RemoveStep(args, d, s, account)
		if err != nil {
			return "", err
		}
		if !removed {
			return "it was gone already", nil
		}
		return "removed", nil
	}
	switch s.Kind {
	case infra.KindHost:
		h, err := infranodes.EnsureHost(args, d, s, account, "")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("host %s is %s", h.Record, h.State), nil
	case infra.KindInstance:
		inst, err := infranodes.EnsureInstance(args, d, s, "")
		if err != nil {
			return "", err
		}
		raw, _ := json.Marshal(s.Desired["units"])
		var units []infra.Unit
		_ = json.Unmarshal(raw, &units)
		for _, u := range units {
			if _, _, err := infranodes.EnsureUnit(args, d, inst.Record, u); err != nil {
				return "", err
			}
		}
		raw, _ = json.Marshal(s.Desired["packages"])
		var pkgs []string
		_ = json.Unmarshal(raw, &pkgs)
		if len(pkgs) > 0 {
			if _, _, err := infranodes.EnsurePackages(args, d, inst.Profile, "", pkgs); err != nil {
				return "", err
			}
		}
		return "instance " + inst.Record + " is in the desired state", nil
	case infra.KindNatsNode:
		n, err := infranodes.EnsureNatsNode(args, d, s)
		if err != nil {
			return "", err
		}
		return "NATS node " + n.Instance + " is in " + n.Cluster, nil
	case infra.KindStreams:
		tenant, cluster := fmt.Sprint(s.Desired["tenant"]), fmt.Sprint(s.Desired["cluster"])
		rec, err := infranodes.EnsureStreams(args, d, tenant, cluster)
		if err != nil {
			return "", err
		}
		return "tenant " + tenant + "'s streams are placed in " + cluster + " (" + rec + "); the Control Plane moves them one at a time", nil
	case infra.KindGateway:
		// The sites' NATS nodes write the gateways into their cluster
		// records; this step only says so once they have.
		return "the sites' NATS clusters are joined", nil
	}
	return "nothing to do for a " + s.Kind + " yet", nil
}
