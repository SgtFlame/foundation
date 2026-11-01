// Package bus decides and commits commands (documentation/Live.md ›
// Commands): it authorizes a command, reads its target's projected state,
// runs Decide and publishes the events with JetStream's expected last
// subject sequence, deciding again when someone else committed in between.
package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/nats-io/nats.go/jetstream"
)

var log = live.Log.Tags("bus")

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
	RequestID string // groups the commands of one gesture or chat round
	Args      json.RawMessage
}

// Result is a commit's sequence, or the problems that rejected it.
type Result struct {
	Common    features.ServiceResponse
	Stream    string
	Seq       uint64 // the last event's sequence; the state's when nothing was committed
	Duplicate bool   // the command was committed before, under the same id
	Problems  []live.Problem
}

// Bus decides commands. Commands on one target are decided one at a time
// in this process, so a busy target rarely conflicts.
type Bus struct {
	p     *project.Projections
	mu    sync.Mutex
	locks map[string]*target
	// Committed hears every commit, for tracing and tests.
	Committed func(r Request, res Result)
}

type target struct {
	mu   sync.Mutex
	last uint64 // the last sequence this process committed on the target
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

// Command finds a command type.
func (b *Bus) Command(typ string) (live.Command, bool) {
	for _, c := range live.Commands.All(b.p.Registry()) {
		if c.ID == typ {
			return c, true
		}
	}
	return live.Command{}, false
}

func rejected(problems ...live.Problem) Result {
	return Result{Common: features.ServiceResponse{Code: http.StatusUnprocessableEntity, Message: problems[0].Message, ErrorCode: "command_rejected"}, Problems: problems}
}

// Execute decides and commits one command.
func (b *Bus) Execute(r Request) Result {
	cmd, ok := b.Command(r.Type)
	if !ok {
		return Result{Common: *r.Fail(http.StatusNotFound, log.Code("unknown_command").Errorf("there is no command %q", r.Type))}
	}
	if r.ID == "" {
		return rejected(live.Problem{Path: "id", Message: "a command needs an id", Fix: "send a new unique id with every command"})
	}
	if !live.ValidToken(r.Target) {
		return rejected(live.Problem{Path: "target", Message: fmt.Sprintf("%q is not a valid target id", r.Target), Fix: "use letters, digits, '-' and '_' only"})
	}
	tg := live.Target{Area: cmd.Area, Type: cmd.TargetType, ID: r.Target}
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
	lock := b.lock(fmt.Sprintf("%d/%s/%s", tenant, cmd.Area, tg.Key()))
	lock.mu.Lock()
	defer lock.mu.Unlock()

	wait := lock.last
	for try := 0; try < Tries; try++ {
		state, err := b.p.WaitFor(ctx, tenant, cmd.Projection, tg.Key(), wait, catchUp)
		if err != nil {
			return Result{Common: *r.Fail(http.StatusInternalServerError, err)}
		}
		events, problems := cmd.Decide(live.Decision{ServiceArgs: r.ServiceArgs, Target: tg, State: state.Value, Args: r.Args})
		if len(problems) > 0 {
			return rejected(problems...)
		}
		if len(events) == 0 {
			return Result{Common: features.OK(), Stream: streamName, Seq: state.Seq}
		}
		res, conflict, err := b.commit(r, tg, streamName, state.Seq, events)
		if conflict {
			wait = max(state.Seq+1, conflictSeq(err)) // someone committed since: decide again once projected
			continue
		}
		if err != nil {
			return Result{Common: *r.Fail(http.StatusInternalServerError, log.Wrapf(err, "cannot commit %s", r.Type))}
		}
		lock.last = res.Seq
		if b.Committed != nil {
			b.Committed(r, res)
		}
		return res
	}
	return Result{Common: *r.Fail(http.StatusConflict, log.Code("conflict").Errorf("%s on %s kept conflicting with other changes; try again", r.Type, tg.Key()))}
}

// commit publishes the events, the first expecting the target's last
// sequence to be the one the state was read at, and each later one the
// previous one's. A command's several events are not yet one atomic batch
// (03-DataModel question 4).
func (b *Bus) commit(r Request, tg live.Target, stream string, seq uint64, events []live.NewEvent) (Result, bool, error) {
	ctx := r.Context()
	tenant := r.Principal.TenantID
	res := Result{Common: features.OK(), Stream: stream}
	for i, ne := range events {
		data, err := json.Marshal(ne.Data)
		if err != nil {
			return res, false, err
		}
		e := live.Event{Type: ne.Type, Target: tg, Data: data, CommandID: r.ID, RequestID: r.RequestID, UserID: r.Principal.UserID, TenantID: tenant, Time: time.Now().UTC()}
		raw, _ := json.Marshal(e)
		msgID := r.ID
		if i > 0 {
			msgID = fmt.Sprintf("%s#%d", r.ID, i)
		}
		ack, err := b.p.JetStream().Publish(ctx, tg.Subject(tenant, ne.Type), raw,
			jetstream.WithMsgID(msgID), jetstream.WithExpectStream(stream),
			jetstream.WithExpectLastSequenceForSubject(seq, tg.Subjects(tenant)))
		if err != nil {
			var apiErr *jetstream.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence {
				if i == 0 {
					return res, true, err
				}
			}
			return res, false, err
		}
		if ack.Duplicate {
			res.Duplicate = true
		}
		seq = ack.Sequence
		res.Seq = ack.Sequence
	}
	return res, false, nil
}

// conflictSeq reads the target's actual last sequence from a wrong last
// sequence error ("wrong last sequence: 42"), or 0.
func conflictSeq(err error) uint64 {
	var n uint64
	if err != nil {
		var apiErr *jetstream.APIError
		if errors.As(err, &apiErr) {
			_, _ = fmt.Sscanf(apiErr.Description, "wrong last sequence: %d", &n)
		}
	}
	return n
}
