// Package bus decides and commits commands (documentation/Live.md ›
// Commands): it authorizes a command, reads its target's projected state,
// runs Decide and publishes the events with JetStream's expected last
// subject sequence, deciding again when someone else committed in between.
// A command's several events, on its target and on others it read, are
// published as one atomic batch.
package bus

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var log = live.Log.Tags("bus")

// busyTries is how often a batch the stream refused as one too many in
// flight is sent again before the command fails.
const busyTries = 50

// Tries is how often a command is decided before it fails with conflict.
const Tries = 3

// catchUp is how long a decision waits for the projection to reflect the
// target's last commit.
const catchUp = 2 * time.Second

// Request is one command as sent.
type Request struct {
	features.ServiceArgs
	ID        string // the idempotency key
	Type      string
	Target    string // the target's id; its area and type come from the command
	// RequestID groups the commands of one gesture or chat round; a
	// command sent without one is its own request (forge 06a question 1).
	RequestID string
	// App is the app the request was made for, which a definition command
	// on a part no app owns is logged under (forge 06a question 4).
	App string
	// Actor is who sends the command for the principal: the chat.
	Actor string
	Args  json.RawMessage
}

// Result is a commit's sequence, or the problems that rejected it.
type Result struct {
	Common    features.ServiceResponse
	Stream    string
	Target    string // the target's id, made by the command when the request had none
	Seq       uint64 // the last event's sequence; the state's when nothing was committed
	Duplicate bool   // the command was committed before, under the same id
	RequestID string // the request it was logged under
	Problems  []live.Problem
}

// Bus decides commands. Commands on one target are decided one at a time
// in this process, so a busy target rarely conflicts; a command decided
// again after a conflict also holds every other target it changes.
type Bus struct {
	p     *project.Projections
	mu    sync.Mutex
	locks map[string]*target
	// Committed hears every commit, for tracing and tests.
	Committed func(r Request, res Result)
	// Resolve finds a command no feature registered, such as one a
	// tenant's published command graph defines (live/nodes).
	Resolve func(args features.ServiceArgs, typ string) (live.Command, bool)
}

type target struct {
	mu   sync.Mutex
	last atomic.Uint64 // the last sequence this process committed on the target
}

func New(p *project.Projections) *Bus {
	return &Bus{p: p, locks: map[string]*target{}}
}

func (b *Bus) lock(key string) *target {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.locks[key]
	if t == nil {
		t = &target{}
		b.locks[key] = t
	}
	return t
}

// Command finds a registered command type.
func (b *Bus) Command(typ string) (live.Command, bool) {
	for _, c := range live.Commands.All(b.p.Registry()) {
		if c.ID == typ {
			return c, true
		}
	}
	return live.Command{}, false
}

// CommandFor finds a command type as the principal's tenant has it: a
// registered one, else one Resolve finds.
func (b *Bus) CommandFor(args features.ServiceArgs, typ string) (live.Command, bool) {
	if c, ok := b.Command(typ); ok {
		return c, true
	}
	if b.Resolve != nil {
		return b.Resolve(args, typ)
	}
	return live.Command{}, false
}

func rejected(problems ...live.Problem) Result {
	code := "command_rejected"
	if problems[0].Code != "" {
		code = problems[0].Code
	}
	return Result{Common: features.ServiceResponse{Code: http.StatusUnprocessableEntity, Message: problems[0].Message, ErrorCode: code}, Problems: problems}
}

// Execute decides and commits one command.
func (b *Bus) Execute(r Request) Result {
	cmd, ok := b.CommandFor(r.ServiceArgs, r.Type)
	if !ok {
		return Result{Common: *r.Fail(http.StatusNotFound, log.Code("unknown_command").Errorf("there is no command %q", r.Type))}
	}
	if r.ID == "" {
		return rejected(live.Problem{Path: "id", Message: "a command needs an id", Fix: "send a new unique id with every command"})
	}
	if r.RequestID == "" {
		r.RequestID = r.ID
	}
	if r.App != "" && !live.ValidToken(r.App) {
		return rejected(live.Problem{Path: "app", Message: fmt.Sprintf("%q is not a valid app id", r.App)})
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

	ctx := r.Context()
	tenant := r.Principal.TenantID
	stream, err := b.p.Stream(ctx, tenant, cmd.Area)
	if err != nil {
		return Result{Common: *r.Fail(http.StatusInternalServerError, err)}
	}
	streamName := stream.CachedInfo().Config.Name
	// The command's target is locked, and after a conflict every target its
	// events change, so the next decision is the only one in this process
	keys := []string{b.lockKey(tenant, tg)}
	if cmd.Holds != nil {
		for _, t := range cmd.Holds(live.Decision{ServiceArgs: r.ServiceArgs, Target: tg, Args: r.Args}) {
			if t.Area == cmd.Area {
				keys = append(keys, b.lockKey(tenant, t))
			}
		}
	}
	held := b.hold(nil, keys)
	defer func() { b.release(held) }()

	// wait is the sequence each target's projection must reflect before
	// it is read: this process's last commit on it, or what a conflict
	// showed was committed since
	wait := map[string]uint64{}
	waitFor := func(t live.Target) uint64 {
		return max(wait[t.Key()], b.lock(b.lockKey(tenant, t)).last.Load())
	}
	busy := 0
	for try := 0; try < Tries; try++ {
		state, err := b.p.WaitFor(ctx, tenant, cmd.Projection, tg.Key(), waitFor(tg), catchUp)
		if err != nil {
			return Result{Common: *r.Fail(http.StatusInternalServerError, err)}
		}
		read := map[string]uint64{tg.Key(): state.Seq} // the sequence each target was read at
		d := live.Decision{ServiceArgs: r.ServiceArgs, Target: tg, State: state.Value, Args: r.Args}
		d.Read = func(projection string, t live.Target) (json.RawMessage, error) {
			if t.Area != cmd.Area {
				return nil, log.Code("other_area").Errorf("%s changes targets in the %s area only, not %s", r.Type, cmd.Area, t.Area)
			}
			v, err := b.p.WaitFor(ctx, tenant, projection, t.Key(), waitFor(t), catchUp)
			if err != nil {
				return nil, err
			}
			if seq, ok := read[t.Key()]; !ok || v.Seq < seq {
				read[t.Key()] = v.Seq
			}
			return v.Value, nil
		}
		if cmd.Logged() {
			d.Op = &live.OpInfo{}
		}
		events, problems := cmd.Decide(d)
		if len(problems) > 0 {
			return rejected(problems...)
		}
		if len(events) == 0 {
			return Result{Common: features.OK(), Stream: streamName, Target: tg.ID, Seq: state.Seq, RequestID: r.RequestID}
		}
		var op *live.LoggedOp
		if d.Op != nil {
			op = &live.LoggedOp{Command: r.Type, Target: tg.ID, Args: r.Args, App: cmp.Or(d.Op.App, r.App),
				Inverse: d.Op.Inverse, Irreversible: d.Op.Irreversible, Decided: state.Seq}
			if op.Inverse == nil {
				op.Inverse = []live.Op{}
			}
		}
		res, conflict, err := b.commit(r, cmd, tg, stream, read, events, op)
		if conflict {
			// Someone committed since: decide again once projected
			var keys []string
			for _, t := range targets(tg, events) {
				wait[t.Key()] = max(wait[t.Key()], read[t.Key()]+1, b.lastSeq(ctx, stream, tenant, t))
				keys = append(keys, b.lockKey(tenant, t))
			}
			held = b.hold(held, keys)
			continue
		}
		if project.IsBusy(err) && busy < busyTries {
			// Nothing was stored: send it again once a batch has landed
			busy++
			try--
			time.Sleep(time.Duration(busy*5+rand.IntN(20)) * time.Millisecond)
			continue
		}
		if err != nil {
			if p, ok := problemOf(err); ok {
				return rejected(p)
			}
			return Result{Common: *r.Fail(http.StatusInternalServerError, log.Wrapf(err, "cannot commit %s", r.Type))}
		}
		if b.Committed != nil {
			b.Committed(r, res)
		}
		return res
	}
	return Result{Common: *r.Fail(http.StatusConflict, log.Code("conflict").Errorf("%s on %s kept conflicting with other changes; try again", r.Type, tg.Key()))}
}

// hold locks keys, keeping those held already. Locks are taken in key
// order, all again when one more is needed, so two commands waiting on each
// other's targets cannot deadlock; a command holding one lock never waits
// for another.
func (b *Bus) hold(held map[string]*target, keys []string) map[string]*target {
	missing := false
	for _, k := range keys {
		if held[k] == nil {
			missing = true
		}
	}
	if !missing {
		return held
	}
	all := slices.Collect(maps.Keys(held))
	for _, k := range keys {
		if held[k] == nil {
			all = append(all, k)
		}
	}
	slices.Sort(all)
	b.release(held)
	out := map[string]*target{}
	for _, k := range all {
		t := b.lock(k)
		t.mu.Lock()
		out[k] = t
	}
	return out
}

func (b *Bus) release(held map[string]*target) {
	for _, t := range held {
		t.mu.Unlock()
	}
}

func (b *Bus) lockKey(tenant uint64, t live.Target) string {
	return fmt.Sprintf("%d/%s/%s", tenant, t.Area, t.Key())
}

// targets is every target the events change, the command's own first.
func targets(tg live.Target, events []live.NewEvent) []live.Target {
	out := []live.Target{tg}
	for _, ne := range events {
		if ne.Target != nil && !slices.Contains(out, *ne.Target) {
			out = append(out, *ne.Target)
		}
	}
	return out
}

// lastSeq is the sequence of the target's last event in the stream, 0
// when it has none.
func (b *Bus) lastSeq(ctx context.Context, stream jetstream.Stream, tenant uint64, t live.Target) uint64 {
	m, err := stream.GetLastMsgForSubject(ctx, t.Subjects(tenant))
	if err != nil {
		return 0
	}
	return m.Sequence
}

// badTarget is an event's target the command may not change.
type badTarget struct{ problem live.Problem }

func (e badTarget) Error() string { return e.problem.Message }

func problemOf(err error) (live.Problem, bool) {
	var bt badTarget
	if errors.As(err, &bt) {
		return bt.problem, true
	}
	return live.Problem{}, false
}

// commit publishes the events, each expecting its target's last sequence
// to be the one it was read at; a target the decision did not read is
// expected as the stream has it now. One event is published alone, and
// several as one atomic batch: every check passes and all are stored, or
// none is.
func (b *Bus) commit(r Request, cmd live.Command, tg live.Target, stream jetstream.Stream, read map[string]uint64, events []live.NewEvent, op *live.LoggedOp) (Result, bool, error) {
	ctx := r.Context()
	tenant := r.Principal.TenantID
	streamName := stream.CachedInfo().Config.Name
	res := Result{Common: features.OK(), Stream: streamName, Target: tg.ID, RequestID: r.RequestID}
	if len(events) > project.MaxBatch {
		return res, false, badTarget{live.Problem{Message: fmt.Sprintf("a command commits at most %d events, not %d", project.MaxBatch, len(events)), Fix: "split the change into several commands"}}
	}
	now := time.Now().UTC()
	msgs := make([]*nats.Msg, len(events))
	evTargets := make([]live.Target, len(events))
	for i, ne := range events {
		t := tg
		if ne.Target != nil {
			t = *ne.Target
			if t.Area != cmd.Area || !live.ValidToken(t.Type) || !live.ValidToken(t.ID) {
				return res, false, badTarget{live.Problem{Path: "target", Message: fmt.Sprintf("%s cannot change %s.%s in area %s", r.Type, t.Type, t.ID, t.Area), Fix: "a command changes targets in its own area, with valid ids"}}
			}
		}
		evTargets[i] = t
		data, err := json.Marshal(ne.Data)
		if err != nil {
			return res, false, err
		}
		e := live.Event{Type: ne.Type, Target: t, Data: data, CommandID: r.ID, RequestID: r.RequestID, UserID: r.Principal.UserID, Actor: r.Actor, TenantID: tenant, Time: now}
		if i == 0 {
			e.Op = op
		}
		if len(events) > 1 {
			e.Batch = &live.Batch{Index: i + 1, Count: len(events)}
		}
		raw, _ := json.Marshal(e)
		seq, ok := read[t.Key()]
		if !ok {
			seq = b.lastSeq(ctx, stream, tenant, t)
			read[t.Key()] = seq
		}
		m := nats.NewMsg(t.Subject(tenant, ne.Type))
		m.Data = raw
		msgID := r.ID
		if i > 0 {
			msgID = fmt.Sprintf("%s#%d", r.ID, i)
		}
		m.Header.Set(jetstream.MsgIDHeader, msgID)
		m.Header.Set(jetstream.ExpectedStreamHeader, streamName)
		m.Header.Set(jetstream.ExpectedLastSubjSeqHeader, strconv.FormatUint(seq, 10))
		m.Header.Set(jetstream.ExpectedLastSubjSeqSubjHeader, t.Subjects(tenant))
		msgs[i] = m
	}
	if len(msgs) == 1 {
		ack, err := b.p.JetStream().PublishMsg(ctx, msgs[0])
		if err != nil {
			return res, project.IsConflict(err), err
		}
		res.Seq, res.Duplicate = ack.Sequence, ack.Duplicate
		b.lock(b.lockKey(tenant, evTargets[0])).last.Store(ack.Sequence)
		return res, false, nil
	}
	ack, err := project.PublishBatch(ctx, b.p.JetStream().Conn(), msgs)
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode == project.ErrCodeBatchDuplicate {
		// Committed before under the same id. The batch's own sequence is
		// not reported, so the ack names its targets' last since, which a
		// client waiting for its write reaches no earlier
		res.Duplicate = true
		for _, t := range targets(tg, events) {
			res.Seq = max(res.Seq, b.lastSeq(ctx, stream, tenant, t))
		}
		return res, false, nil
	}
	if err != nil {
		return res, project.IsConflict(err), err
	}
	// The batch is stored in one run, ending at the ack's sequence
	first := ack.Seq - uint64(len(msgs)) + 1
	for i, t := range evTargets {
		b.lock(b.lockKey(tenant, t)).last.Store(first + uint64(i))
	}
	res.Seq = ack.Seq
	return res, false, nil
}
