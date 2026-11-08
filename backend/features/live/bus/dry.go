package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Overlay is projected values as events not committed leave them, by
// projector and key (forge 06f): a command graph's reads see its own
// writes so far, and a dry run's reads its own commands' events. Only the
// projectors keyed by target fold into it; a projector's Keep is not run.
type Overlay struct {
	projectors []live.Projector
	mu         sync.Mutex
	values     map[string]json.RawMessage // nil: the key has no value now
}

// NewOverlay is an empty overlay over the registry's projectors.
func (b *Bus) NewOverlay() *Overlay {
	var prs []live.Projector
	for _, pr := range live.Projectors.All(b.p.Registry()) {
		if pr.KeyOf == nil {
			prs = append(prs, pr)
		}
	}
	return &Overlay{projectors: prs, values: map[string]json.RawMessage{}}
}

// Get is a key's value in the overlay; ok is false when no event folded
// into it, and value nil when one left it without a value.
func (o *Overlay) Get(projector, key string) (value json.RawMessage, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	v, ok := o.values[overlayKey(projector, key)]
	return v, ok
}

// Matching is the overlay's keys of a projector that match a KV key
// pattern ("task.*"), with their values.
func (o *Overlay) Matching(projector, pattern string) map[string]json.RawMessage {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := map[string]json.RawMessage{}
	for k, v := range o.values {
		pr, key, _ := strings.Cut(k, "|")
		if pr == projector && keyMatches(pattern, key) {
			out[key] = v
		}
	}
	return out
}

// keyMatches reports whether a key matches a KV key pattern: "*" is one
// token, ">" the rest.
func keyMatches(pattern, key string) bool {
	if strings.HasSuffix(pattern, ">") {
		return strings.HasPrefix(key, strings.TrimSuffix(pattern, ">"))
	}
	ok, _ := path.Match(strings.ReplaceAll(pattern, ".", "/"), strings.ReplaceAll(key, ".", "/"))
	return ok
}

// Apply folds an event into every projector of its area that keeps its
// target, reading a value the overlay does not have yet with base.
func (o *Overlay) Apply(e live.Event, base func(projector string, t live.Target) (json.RawMessage, error)) error {
	for _, pr := range o.projectors {
		if pr.Area != e.Target.Area {
			continue
		}
		if len(pr.Types) > 0 && !slices.Contains(pr.Types, e.Target.Type) {
			continue
		}
		cur, ok := o.Get(pr.ID, e.Target.Key())
		if !ok {
			v, err := base(pr.ID, e.Target)
			if err != nil {
				return err
			}
			cur = v
		}
		next, err := pr.Fold(live.Fold{Key: e.Target.Key(), State: cur, Event: e})
		if err != nil {
			return err
		}
		o.mu.Lock()
		o.values[overlayKey(pr.ID, e.Target.Key())] = next
		o.mu.Unlock()
	}
	return nil
}

// Dry decides commands as Execute does, but commits nothing (forge 06f):
// each is decided against the projections with the events of the commands
// before it folded over them, and its events are kept in memory. A dry run
// is a sandbox: nothing reaches the stream or the projections, and it is
// dropped when the run ends. Every target reads as the projections have it
// now; a dry run does not wait for this process's last commit.
type Dry struct {
	// Names maps the command types the run sends to those decided, as an
	// app's page does (an app's task.reassign is draft.tasks.task.reassign
	// in its draft run).
	Names map[string]string
	b     *Bus
	over  *Overlay
	mu    sync.Mutex
	evts  []live.Event
	cmds  []DryCommand
	n     int
}

// DryCommand is a command a dry run decided: the events it would commit,
// or why it was refused.
type DryCommand struct {
	Command  string          `json:"command"`
	Target   string          `json:"target"`
	Args     json.RawMessage `json:"args,omitempty"`
	Events   int             `json:"events"`
	Problems []live.Problem  `json:"problems,omitempty"`
	Error    string          `json:"error,omitempty"` // a refusal without problems
}

// Commands are the commands the dry run decided, in order.
func (d *Dry) Commands() []DryCommand {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.cmds)
}

// Dry starts a dry run.
func (b *Bus) Dry() *Dry { return &Dry{b: b, over: b.NewOverlay()} }

// Events are the events the dry run's commands decided, in order.
func (d *Dry) Events() []live.Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.evts)
}

type dryKey struct{}

// WithDry is a context whose commands and record reads go to the dry run:
// Send command, the record nodes and Read projection look for it.
func WithDry(ctx context.Context, d *Dry) context.Context { return context.WithValue(ctx, dryKey{}, d) }

// DryOf is the dry run a context carries, nil for none.
func DryOf(ctx context.Context) *Dry {
	d, _ := ctx.Value(dryKey{}).(*Dry)
	return d
}

// Get is a projected value as the dry run has it: its own events over the
// projections.
func (d *Dry) Get(ctx context.Context, tenant uint64, projector, key string) (live.Projected, bool, error) {
	if v, ok := d.over.Get(projector, key); ok {
		return live.Projected{Value: v}, v != nil, nil
	}
	return d.b.p.Get(ctx, tenant, projector, key)
}

// Watch sends the keys matching pattern as the dry run has them, then nil,
// and closes when ctx ends; it sends no later changes, since a dry run's
// reads are one at a time.
func (d *Dry) Watch(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *live.Entry, error) {
	mine := d.over.Matching(projector, pattern)
	baseCtx, cancel := context.WithCancel(ctx)
	base, err := d.b.p.Watch(baseCtx, tenant, projector, pattern)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan *live.Entry)
	go func() {
		defer close(out)
		send := func(e *live.Entry) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for e := range base {
			if e == nil {
				break
			}
			if _, ok := mine[e.Key]; ok {
				continue
			}
			if !send(e) {
				cancel()
				return
			}
		}
		cancel() // the base's current keys are read
		keys := make([]string, 0, len(mine))
		for k := range mine {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if mine[k] == nil {
				continue
			}
			if !send(&live.Entry{Key: k, Value: live.Projected{Value: mine[k]}, Initial: true}) {
				return
			}
		}
		send(nil)
	}()
	return out, nil
}

func (d *Dry) value(ctx context.Context, tenant uint64, projector string, t live.Target) (json.RawMessage, error) {
	v, _, err := d.Get(ctx, tenant, projector, t.Key())
	return v.Value, err
}

// Execute decides one command in the dry run: the command the principal's
// tenant has (Resolve's too).
func (d *Dry) Execute(r Request) Result {
	if typ, ok := d.Names[r.Type]; ok {
		r.Type = typ
	}
	cmd, ok := d.b.CommandFor(r.ServiceArgs, r.Type)
	if !ok {
		return Result{Common: *r.Fail(http.StatusNotFound, log.Code("unknown_command").Errorf("there is no command %q", r.Type))}
	}
	return d.Decide(r, cmd)
}

// Decide decides one command, cmd, in the dry run, checked as Execute
// checks it: its events fold into the run and are kept, or its problems
// are returned.
func (d *Dry) Decide(r Request, cmd live.Command) Result {
	before := len(d.Events())
	res := d.decide(&r, cmd)
	c := DryCommand{Command: r.Type, Target: res.Target, Args: r.Args, Events: len(d.Events()) - before, Problems: res.Problems}
	if c.Target == "" {
		c.Target = r.Target
	}
	if res.Common.Code >= 300 && len(res.Problems) == 0 {
		c.Error = res.Common.Message
	}
	d.mu.Lock()
	d.cmds = append(d.cmds, c)
	d.mu.Unlock()
	return res
}

func (d *Dry) decide(rp *Request, cmd live.Command) Result {
	r := *rp
	defer func() { *rp = r }()
	if r.Type == "" {
		r.Type = cmd.ID
	}
	d.mu.Lock()
	d.n++
	n := d.n
	d.mu.Unlock()
	if r.ID == "" {
		r.ID = "dry-" + strconv.Itoa(n)
	}
	if r.RequestID == "" {
		r.RequestID = r.ID
	}
	if r.Target == "" && cmd.NewTarget != nil {
		r.Target = cmd.NewTarget()
	}
	if r.Target == "" && cmd.NewTargetFor != nil {
		r.Target = cmd.NewTargetFor(r.ServiceArgs, r.Args)
	}
	if !live.ValidToken(r.Target) {
		return rejected(live.Problem{Path: "target", Message: fmt.Sprintf("%q is not a valid target id", r.Target), Fix: "use letters, digits, '-' and '_' only"})
	}
	tg, problem := cmd.TargetFor(r.Target, r.Args)
	if problem != nil {
		return rejected(*problem)
	}
	resource := authz.Tenant(r.Principal)
	if cmd.Resource != nil {
		resource = cmd.Resource(r.Principal, tg)
	}
	if err := authz.Check(r.Context(), r.Principal, cmd.Action, resource).Err(cmd.Action, resource); err != nil {
		return Result{Common: *r.Fail(authz.Status, err)}
	}
	if err := tools.CheckArgs(cmd.Args, r.Args); err != nil {
		return rejected(live.Problem{Path: "args", Message: err.Error()})
	}
	ctx, tenant := r.Context(), r.Principal.TenantID
	state, err := d.value(ctx, tenant, cmd.Projection, tg)
	if err != nil {
		return Result{Common: *r.Fail(http.StatusInternalServerError, err)}
	}
	dec := live.Decision{ServiceArgs: r.ServiceArgs, Target: tg, State: state, Args: r.Args}
	dec.Read = func(projection string, t live.Target) (json.RawMessage, error) {
		if t.Area != cmd.Area {
			return nil, log.Code("other_area").Errorf("%s changes targets in the %s area only, not %s", r.Type, cmd.Area, t.Area)
		}
		return d.value(ctx, tenant, projection, t)
	}
	if cmd.Logged() {
		dec.Op = &live.OpInfo{}
	}
	events, problems := cmd.Decide(dec)
	if len(problems) > 0 {
		return rejected(problems...)
	}
	now := time.Now().UTC()
	var decided []live.Event
	for _, ne := range events {
		t := tg
		if ne.Target != nil {
			t = *ne.Target
			if t.Area != cmd.Area || !live.ValidToken(t.Type) || !live.ValidToken(t.ID) {
				return rejected(live.Problem{Path: "target", Message: fmt.Sprintf("%s cannot change %s.%s in area %s", r.Type, t.Type, t.ID, t.Area), Fix: "a command changes targets in its own area, with valid ids"})
			}
		}
		data, err := json.Marshal(ne.Data)
		if err != nil {
			return Result{Common: *r.Fail(http.StatusInternalServerError, err)}
		}
		e := live.Event{Type: ne.Type, Target: t, Data: data, CommandID: r.ID, RequestID: r.RequestID, UserID: r.Principal.UserID, Actor: r.Actor, TenantID: tenant, Time: now}
		if err := d.over.Apply(e, func(projector string, t live.Target) (json.RawMessage, error) {
			return d.value(ctx, tenant, projector, t)
		}); err != nil {
			return rejected(live.Problem{Message: fmt.Sprintf("%s cannot be applied: %v", r.Type, err)})
		}
		decided = append(decided, e)
	}
	d.mu.Lock()
	d.evts = append(d.evts, decided...)
	d.mu.Unlock()
	return Result{Common: features.OK(), Target: tg.ID, RequestID: r.RequestID}
}
