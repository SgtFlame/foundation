package bus

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Decided is one op of a transaction as decided: the events it would
// commit, none when it changes nothing.
type Decided struct {
	Op     live.Op
	Events int
}

// TransactResult is a transaction's commit, or the problems of the op that
// refused it (their paths start ops.<index>).
type TransactResult struct {
	Result
	Ops []Decided
}

// overlay is the projected values a transaction's ops have changed so far,
// by projector and key: each op is decided against the state the ops
// before it left, although nothing is committed yet.
type overlay map[string]json.RawMessage

func overlayKey(projector, key string) string { return projector + "|" + key }

// Transact decides ops one after another, each against the state the ops
// before it left, and commits all their events as one atomic batch in
// r's request (forge 06b: an undo is one request, committed atomically;
// forge 06e: forge.apply's batch). Every op must be in one area; an op
// with no target, of a command that makes its own, gets a new one, which
// its Decided names. With dryRun nothing is committed: the
// result says what each op would do, or which refused.
func (b *Bus) Transact(r Request, ops []live.Op, dryRun bool) TransactResult {
	if r.ID == "" {
		return TransactResult{Result: rejected(live.Problem{Path: "id", Message: "a transaction needs an id"})}
	}
	if r.RequestID == "" {
		r.RequestID = r.ID
	}
	if len(ops) == 0 {
		return TransactResult{Result: Result{Common: features.OK(), RequestID: r.RequestID}}
	}
	if r.App != "" && !live.ValidToken(r.App) {
		return TransactResult{Result: rejected(live.Problem{Path: "app", Message: fmt.Sprintf("%q is not a valid app id", r.App)})}
	}
	type step struct {
		op  live.Op
		cmd live.Command
		tg  live.Target
	}
	steps := make([]step, len(ops))
	var area live.Area
	for i, op := range ops {
		at := func(p live.Problem) TransactResult {
			p.Path = strings0(fmt.Sprintf("ops.%d", i), p.Path)
			return TransactResult{Result: rejected(p)}
		}
		cmd, ok := b.CommandFor(r.ServiceArgs, op.Command)
		if !ok {
			return at(live.Problem{Path: "command", Message: fmt.Sprintf("there is no command %q", op.Command)})
		}
		if i == 0 {
			area = cmd.Area
		} else if cmd.Area != area {
			return at(live.Problem{Message: fmt.Sprintf("%s is in the %s area, not %s: a transaction is in one area", op.Command, cmd.Area, area)})
		}
		args := op.Args
		if args == nil {
			args = json.RawMessage(`{}`)
		}
		ops[i].Args = args
		// A command that makes its target names it when the op does not
		// (forge 06e: a batch creates an entity type and adds to it)
		if op.Target == "" && cmd.NewTarget != nil {
			ops[i].Target = cmd.NewTarget()
		}
		if ops[i].Target == "" && cmd.NewTargetFor != nil {
			ops[i].Target = cmd.NewTargetFor(r.ServiceArgs, args)
		}
		op = ops[i]
		if !live.ValidToken(op.Target) {
			return at(live.Problem{Path: "target", Message: fmt.Sprintf("%q is not a valid target id", op.Target), Fix: "use letters, digits, '-' and '_' only"})
		}
		tg, problem := cmd.TargetFor(op.Target, args)
		if problem != nil {
			return at(*problem)
		}
		resource := authz.Tenant(r.Principal)
		if cmd.Resource != nil {
			resource = cmd.Resource(r.Principal, tg)
		}
		if err := authz.Check(r.Context(), r.Principal, cmd.Action, resource).Err(cmd.Action, resource); err != nil {
			return TransactResult{Result: Result{Common: *r.Fail(authz.Status, err)}}
		}
		if err := tools.CheckArgs(cmd.Args, args); err != nil {
			return at(live.Problem{Path: "args", Message: err.Error()})
		}
		steps[i] = step{op: ops[i], cmd: cmd, tg: tg}
	}

	ctx := r.Context()
	tenant := r.Principal.TenantID
	stream, err := b.p.Stream(ctx, tenant, area)
	if err != nil {
		return TransactResult{Result: Result{Common: *r.Fail(http.StatusInternalServerError, err)}}
	}
	streamName := stream.CachedInfo().Config.Name
	var keys []string
	for _, s := range steps {
		if k := b.lockKey(tenant, s.tg); !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	held := b.hold(nil, keys)
	defer func() { b.release(held) }()

	var projectors []live.Projector
	for _, pr := range live.Projectors.All(b.p.Registry()) {
		if pr.Area == area && pr.KeyOf == nil {
			projectors = append(projectors, pr)
		}
	}
	wait := map[string]uint64{}
	waitFor := func(t live.Target) uint64 {
		return max(wait[t.Key()], b.lock(b.lockKey(tenant, t)).last.Load())
	}
	busy := 0
	for try := 0; try < Tries; try++ {
		over := overlay{}
		read := map[string]uint64{} // the sequence each target was read at in the projections
		// value is a projected value as the ops so far left it
		value := func(projector string, t live.Target) (json.RawMessage, error) {
			if v, ok := over[overlayKey(projector, t.Key())]; ok {
				return v, nil
			}
			v, err := b.p.WaitFor(ctx, tenant, projector, t.Key(), waitFor(t), catchUp)
			if err != nil {
				return nil, err
			}
			if seq, ok := read[t.Key()]; !ok || v.Seq < seq {
				read[t.Key()] = v.Seq
			}
			return v.Value, nil
		}
		type planned struct {
			events []live.NewEvent
			op     *live.LoggedOp
			jobs   []live.Job
		}
		var plan []planned
		var decided []Decided
		var all []live.NewEvent
		var failed *TransactResult
		for i, s := range steps {
			state, err := value(s.cmd.Projection, s.tg)
			if err != nil {
				return TransactResult{Result: Result{Common: *r.Fail(http.StatusInternalServerError, err)}}
			}
			d := live.Decision{ServiceArgs: r.ServiceArgs, Target: s.tg, State: state, Args: s.op.Args}
			d.Read = func(projection string, t live.Target) (json.RawMessage, error) {
				if t.Area != area {
					return nil, log.Code("other_area").Errorf("%s changes targets in the %s area only, not %s", s.op.Command, area, t.Area)
				}
				return value(projection, t)
			}
			if s.cmd.Logged() {
				d.Op = &live.OpInfo{}
			}
			d.Outbox = &live.Outbox{}
			events, problems := s.cmd.Decide(d)
			if len(problems) > 0 {
				for j := range problems {
					problems[j].Path = strings0(fmt.Sprintf("ops.%d", i), problems[j].Path)
				}
				res := TransactResult{Result: rejected(problems...), Ops: decided}
				failed = &res
				break
			}
			for j := range events {
				if events[j].Target == nil {
					t := s.tg
					events[j].Target = &t
				} else if events[j].Target.Area != area {
					res := TransactResult{Result: rejected(live.Problem{Path: fmt.Sprintf("ops.%d.target", i), Message: fmt.Sprintf("%s cannot change a target in area %s", s.op.Command, events[j].Target.Area)})}
					failed = &res
					break
				}
			}
			if failed != nil {
				break
			}
			if len(events) == 0 && len(d.Jobs()) > 0 {
				p := noCarrier(s.op.Command)
				p.Path = fmt.Sprintf("ops.%d", i)
				res := TransactResult{Result: rejected(p), Ops: decided}
				failed = &res
				break
			}
			var op *live.LoggedOp
			if d.Op != nil && len(events) > 0 {
				op = &live.LoggedOp{Command: s.op.Command, Target: s.tg.ID, Args: s.op.Args, App: cmp.Or(d.Op.App, r.App),
					Inverse: d.Op.Inverse, Irreversible: d.Op.Irreversible, Decided: read[s.tg.Key()], Undoes: r.Undoes, Events: len(events)}
				if op.Inverse == nil {
					op.Inverse = []live.Op{}
				}
			}
			// The events fold into the overlay, so the next op sees them
			for _, ne := range events {
				data, err := json.Marshal(ne.Data)
				if err != nil {
					return TransactResult{Result: Result{Common: *r.Fail(http.StatusInternalServerError, err)}}
				}
				e := live.Event{Type: ne.Type, Target: *ne.Target, Data: data, CommandID: r.ID, RequestID: r.RequestID, UserID: r.Principal.UserID, Actor: r.Actor, TenantID: tenant, Time: time.Now().UTC()}
				for _, pr := range projectors {
					if len(pr.Types) > 0 && !slices.Contains(pr.Types, e.Target.Type) {
						continue
					}
					cur, err := value(pr.ID, e.Target)
					if err != nil {
						return TransactResult{Result: Result{Common: *r.Fail(http.StatusInternalServerError, err)}}
					}
					next, err := pr.Fold(live.Fold{Key: e.Target.Key(), State: cur, Event: e})
					if err != nil {
						res := TransactResult{Result: rejected(live.Problem{Path: fmt.Sprintf("ops.%d", i), Message: fmt.Sprintf("%s cannot be applied: %v", s.op.Command, err)})}
						failed = &res
						break
					}
					over[overlayKey(pr.ID, e.Target.Key())] = next
				}
				if failed != nil {
					break
				}
			}
			if failed != nil {
				break
			}
			plan = append(plan, planned{events: events, op: op, jobs: d.Jobs()})
			decided = append(decided, Decided{Op: s.op, Events: len(events)})
			all = append(all, events...)
		}
		if failed != nil {
			return *failed
		}
		out := TransactResult{Result: Result{Common: features.OK(), Stream: streamName, RequestID: r.RequestID}, Ops: decided}
		if dryRun || len(all) == 0 {
			return out
		}
		if len(all) > project.MaxBatch {
			return TransactResult{Result: rejected(live.Problem{Code: "batch_too_large", Message: fmt.Sprintf("a transaction commits at most %d events, not %d", project.MaxBatch, len(all)), Fix: "split it into smaller batches"}), Ops: decided}
		}
		// Build the batch: each op's first event carries it, and each
		// target's first event expects it as it was read
		now := time.Now().UTC()
		var msgs []*nats.Msg
		var evTargets []live.Target
		checked := map[string]bool{}
		for i, p := range plan {
			for j, ne := range p.events {
				t := *ne.Target
				data, _ := json.Marshal(ne.Data)
				e := live.Event{Type: ne.Type, Target: t, Data: data, CommandID: fmt.Sprintf("%s.%d", r.ID, i), RequestID: r.RequestID,
					UserID: r.Principal.UserID, Actor: ActorOf(r), TenantID: tenant, Time: now, Batch: &live.Batch{Index: len(msgs) + 1, Count: len(all)}}
				if j == 0 {
					e.Op, e.Jobs = p.op, p.jobs
				}
				raw, _ := json.Marshal(e)
				m := nats.NewMsg(t.Subject(tenant, ne.Type))
				m.Data = raw
				m.Header.Set(jetstream.MsgIDHeader, fmt.Sprintf("%s#%d", r.ID, len(msgs)))
				m.Header.Set(jetstream.ExpectedStreamHeader, streamName)
				if !checked[t.Key()] {
					checked[t.Key()] = true
					seq, ok := read[t.Key()]
					if !ok {
						seq = b.lastSeq(ctx, stream, tenant, t)
						read[t.Key()] = seq
					}
					m.Header.Set(jetstream.ExpectedLastSubjSeqHeader, strconv.FormatUint(seq, 10))
					m.Header.Set(jetstream.ExpectedLastSubjSeqSubjHeader, t.Subjects(tenant))
				}
				msgs = append(msgs, m)
				evTargets = append(evTargets, t)
			}
		}
		ack, err := project.PublishBatch(ctx, b.p.JetStream().Conn(), msgs)
		if err != nil && project.IsConflict(err) {
			// Someone committed since: decide again once projected
			var keys []string
			for _, t := range evTargets {
				wait[t.Key()] = max(wait[t.Key()], read[t.Key()]+1, b.lastSeq(ctx, stream, tenant, t))
				if k := b.lockKey(tenant, t); !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
			held = b.hold(held, keys)
			continue
		}
		if project.IsBusy(err) && busy < busyTries {
			busy++
			try--
			time.Sleep(time.Duration(busy*5+rand.IntN(20)) * time.Millisecond)
			continue
		}
		if err != nil {
			return TransactResult{Result: Result{Common: *r.Fail(http.StatusInternalServerError, log.Wrapf(err, "cannot commit the transaction"))}}
		}
		first := ack.Seq - uint64(len(msgs)) + 1
		for i, t := range evTargets {
			b.lock(b.lockKey(tenant, t)).last.Store(first + uint64(i))
		}
		out.Seq = ack.Seq
		if b.Committed != nil {
			b.Committed(r, out.Result)
		}
		return out
	}
	return TransactResult{Result: Result{Common: *r.Fail(http.StatusConflict, log.Code("conflict").Errorf("the transaction kept conflicting with other changes; try again"))}}
}

// strings0 joins a path prefix and a path.
func strings0(prefix, path string) string {
	if path == "" {
		return prefix
	}
	return prefix + "." + path
}
