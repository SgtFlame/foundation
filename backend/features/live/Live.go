// Package live is the live core (documentation/Live.md): commands that
// commit events to JetStream, projectors that fold them into KV buckets,
// queries that are subscriptions, presence, and the one WebSocket per
// browser window that carries them. This package holds the types and the
// points features contribute to; natsd, bus, project, subscribe, presence
// and calls hold the machinery.
package live

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Log is the live core's logging and error builder.
var Log = logging.In("live")

// Commands is the live.command point: every command type the bus decides.
var Commands = extensions.NewPoint[Command]("live.command", "Commands: the only way objects change, each deciding events from the target's state")

// Projectors is the live.projector point: folds of event subjects into KV.
var Projectors = extensions.NewPoint[Projector]("live.projector", "Projectors: durable consumers that fold events into a KV projection")

// Queries is the live.query point: named reads that are subscriptions.
var Queries = extensions.NewPoint[Query]("live.query", "Queries: typed reads over projections, answered with a snapshot and then changes")

// Calls is the live.call point: requests answered with a stream of events.
var Calls = extensions.NewPoint[Call]("live.call", "Calls: requests over the live connection answered with a stream of events, such as a chat reply")

// Extensions is the live.extension point: frame handlers attached to every
// live connection, such as UI sessions and collaboration rooms.
var Extensions = extensions.NewPoint[Extension]("live.extension", "Live connection extensions: handle their own frames on every connection, such as ui.* and collab.*")

// Area names a stream: "def" (definitions), "rec" (records) or "app_<appId>".
type Area string

const (
	AreaDefinitions Area = "def"
	AreaRecords     Area = "rec"
)

// AppArea is the area of one application's own aggregates.
func AppArea(appID string) Area { return Area("app_" + appID) }

var token = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidToken reports whether s can be one subject token or key part.
func ValidToken(s string) bool { return token.MatchString(s) }

// StreamName is the JetStream stream of a tenant's area.
func StreamName(tenant uint64, area Area) string {
	return fmt.Sprintf("EVT_%d_%s", tenant, area)
}

// StreamSubjects is what a tenant's area stream holds.
func StreamSubjects(tenant uint64, area Area) string {
	return fmt.Sprintf("evt.%d.%s.>", tenant, area)
}

// Target is the object a command changes: its area, type and id.
type Target struct {
	Area Area   `json:"area"`
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Key is the target's key in its projection, "<type>.<id>".
func (t Target) Key() string { return t.Type + "." + t.ID }

// Subject is the subject of one event on the target.
func (t Target) Subject(tenant uint64, event string) string {
	return fmt.Sprintf("evt.%d.%s.%s.%s.%s", tenant, t.Area, t.Type, t.ID, event)
}

// Subjects is every event subject of the target.
func (t Target) Subjects(tenant uint64) string { return t.Subject(tenant, "*") }

// TargetOf reads the target back from an event subject.
func TargetOf(subject string) (tenant uint64, t Target, event string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[0] != "evt" {
		return 0, Target{}, "", false
	}
	if _, err := fmt.Sscan(parts[1], &tenant); err != nil {
		return 0, Target{}, "", false
	}
	return tenant, Target{Area: Area(parts[2]), Type: parts[3], ID: parts[4]}, parts[5], true
}

// Event is a committed fact, as stored in the stream.
type Event struct {
	Type      string          `json:"type"` // the event name, the subject's last token
	Target    Target          `json:"target"`
	Data      json.RawMessage `json:"data,omitempty"`
	CommandID string          `json:"commandId"`
	RequestID string          `json:"requestId,omitempty"`
	UserID    uint64          `json:"userId,string,omitempty"`
	// Actor is who sent the command for the user, beside them: the chat.
	Actor    string    `json:"actor,omitempty"`
	TenantID uint64    `json:"tenantId,string"`
	Time     time.Time `json:"time"`
	// Batch is set on each event of a commit of several: they were
	// committed together, as one atomic batch.
	Batch *Batch `json:"batch,omitempty"`
	// Op is set on the first event of a definition command: the command as
	// an op of the operation log, with its inverse.
	Op *LoggedOp `json:"op,omitempty"`
	// Jobs is set on the first event of a commit whose command enqueued
	// background jobs (forge 07a): the commit's outbox, which the jobs
	// feature's outbox follower turns into jobs, so a job is enqueued if
	// and only if the command's events commit.
	Jobs []Job `json:"jobs,omitempty"`
	// Set when read back from the stream.
	Stream string `json:"-"`
	Seq    uint64 `json:"-"`
}

// Batch is an event's place in the atomic batch it was committed in: the
// batch's events are stored one after another, so the first is at the
// event's sequence less Index-1.
type Batch struct {
	Index int `json:"index"` // from 1
	Count int `json:"count"`
}

// Range is the stream sequences of the batch an event at seq is in.
func (b Batch) Range(seq uint64) (first, last uint64) {
	first = seq - uint64(b.Index-1)
	return first, first + uint64(b.Count) - 1
}

// Op is one command as the operation log keeps it and undo sends it: its
// type, its target's id and its arguments.
type Op struct {
	Command string          `json:"command"`
	Target  string          `json:"target"`
	Args    json.RawMessage `json:"args,omitempty"`
}

// OpInfo is what a definition command's Decide says about the op it
// decides (forge 06a): the ops that undo it, read from the state it decided
// against, or why it cannot be undone by ops; and the app its target
// belongs to.
type OpInfo struct {
	Inverse      []Op
	Irreversible string // set when no ops undo it: publish, install, import
	App          string // the target's app as decided, "" for a library part
}

// LoggedOp is an op as its first event carries it.
type LoggedOp struct {
	Command string          `json:"command"`
	Target  string          `json:"target"`
	Args    json.RawMessage `json:"args,omitempty"`
	// App is the target's app when decided, else the app the client named
	// the request for (forge 06a question 4); "" for neither.
	App          string `json:"app,omitempty"`
	Inverse      []Op   `json:"inverse"`
	Irreversible string `json:"irreversible,omitempty"`
	// Decided is the target's sequence its state was read at.
	Decided uint64 `json:"decided"`
	// Undoes is the request this op's request undoes (forge 06b): its
	// ops are that request's inverses. A redo undoes an undo.
	Undoes string `json:"undoes,omitempty"`
	// Events is how many events the op committed, when it shares an
	// atomic batch with other ops (an undo's); 0 when the batch is its own.
	Events int `json:"events,omitempty"`
}

// Job is a background job a command enqueues (forge 07a): carried on the
// commit's first event and enqueued by the jobs feature once the events
// are stored, to run as the command's principal.
type Job struct {
	// ID is the job's id and idempotency key: a job with an id already
	// enqueued is not enqueued again.
	ID     string          `json:"id"`
	Graph  string          `json:"graph"`
	Inputs json.RawMessage `json:"inputs,omitempty"`
	Queue  string          `json:"queue,omitempty"`
	// Key, when set, runs the jobs with the same key one at a time, in
	// order (one mail account's syncs).
	Key   string    `json:"key,omitempty"`
	RunAt time.Time `json:"runAt,omitzero"`
	// App is the app whose installed version's copy of Graph runs, when
	// the enqueuing graph is an app's.
	App string `json:"app,omitempty"`
	// As is who the job runs as: the command's principal.
	As features.PrincipalRef `json:"as"`
}

// Outbox holds the jobs a decision enqueues, shared by the decision's
// copies.
type Outbox struct{ Jobs []Job }

// Logged reports whether a command is an op of the operation log: every
// definition command is (forge 06a question 3).
func (c Command) Logged() bool { return c.Area == AreaDefinitions }

// NewEvent is what a command's Decide returns.
type NewEvent struct {
	Type string
	Data any
	// Target, when set, is the event's target in place of the command's: a
	// command may change several targets in its own area, and their events
	// commit together or not at all.
	Target *Target
}

// Problem is why a command was rejected, in a form a model can act on.
type Problem struct {
	// Code, when set, is the rejection's error code in place of
	// command_rejected, such as record_changed.
	Code    string `json:"code,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// Decision is what a command decides on: the target's current projected
// state (nil when it has none) and the arguments.
type Decision struct {
	features.ServiceArgs
	Target Target
	State  json.RawMessage
	Args   json.RawMessage
	// Read is another target's state in a projection (nil when it has
	// none), for a command whose events change it: the commit then expects
	// it unchanged since, as it does the command's own target.
	Read func(projection string, t Target) (json.RawMessage, error)
	// Op is set for a definition command: Decide fills it with the
	// command's inverse and its target's app.
	Op *OpInfo
	// Outbox is set by the bus: the jobs Enqueue adds commit with the
	// events (forge 07a).
	Outbox *Outbox
}

// Enqueue adds a job to the commit's outbox: it is enqueued once the
// command's events are stored, and never if they are not. It reports
// false when the decision has no outbox (a decision made outside the bus).
func (d Decision) Enqueue(j Job) bool {
	if d.Outbox == nil {
		return false
	}
	d.Outbox.Jobs = append(d.Outbox.Jobs, j)
	return true
}

// Jobs are the jobs a decision enqueued.
func (d Decision) Jobs() []Job {
	if d.Outbox == nil {
		return nil
	}
	return d.Outbox.Jobs
}

// Undo sets the op's inverse, when the decision is logged.
func (d Decision) Undo(app string, ops ...Op) {
	if d.Op != nil {
		d.Op.App, d.Op.Inverse = app, ops
	}
}

// NoUndo marks the op as one no op undoes, when the decision is logged.
func (d Decision) NoUndo(app, why string) {
	if d.Op != nil {
		d.Op.App, d.Op.Irreversible = app, why
	}
}

// OpOf is an op of command on target with args given as a Go value.
func OpOf(command, target string, args any) Op {
	raw, _ := json.Marshal(args)
	return Op{Command: command, Target: target, Args: raw}
}

// Command is one command type. Its Descriptor's ID is the type, e.g.
// "hello.increment". Every command is also a tool (effect write).
type Command struct {
	extensions.Descriptor
	Title       string
	Description string            // for a model: what it does and when to use it
	Args        json.RawMessage   // the arguments' JSON Schema, an object
	Examples    []json.RawMessage // example arguments
	Events      []string          // the events it can commit
	Area        Area              // the target's area
	TargetType  string            // the target's type
	// TypeOf, when set, reads the target's type from the arguments, for a
	// command that serves many types: record.* take it from their entity.
	TypeOf func(args json.RawMessage) (string, *Problem)
	// NewTarget, when set, makes the target's id for a command sent
	// without one (a record made from Go or a tool). A browser that may
	// resend the command makes the id itself, so a resend is a duplicate.
	NewTarget func() string
	// NewTargetFor, when set, makes the target's id from the arguments for
	// a command sent without one, such as a prefab's id from its name.
	NewTargetFor func(args features.ServiceArgs, cmdArgs json.RawMessage) string
	// Holds, when set, names other targets the command's events always
	// change, such as a counter every create takes a number from: the bus
	// locks them with the command's own before deciding it, so commands of
	// this process on them do not conflict.
	Holds      func(d Decision) []Target
	Projection string // the projector whose value is the target's state
	// Internal commands are sent only by the server, such as converting a
	// record when its field's type changes: they get no tool.
	Internal bool
	// Action is the authz action it needs, checked on Resource (the
	// principal's tenant when nil).
	Action   string
	Resource func(p features.Principal, target Target) string
	// Decide returns the events to commit, or problems. It may run again
	// when another command committed on the target (or a target it read)
	// in between. Several events commit as one atomic batch.
	Decide func(d Decision) ([]NewEvent, []Problem)
	// Report, when set, is what the command's tool returns after a commit
	// at seq, beside the sequence: a graph draft's problems, for example,
	// so a model sees what its change left to fix.
	Report func(args features.ServiceArgs, target Target, seq uint64) any
}

// TargetFor is the target of the command sent to id with args.
func (c Command) TargetFor(id string, args json.RawMessage) (Target, *Problem) {
	t := Target{Area: c.Area, Type: c.TargetType, ID: id}
	if c.TypeOf != nil {
		typ, p := c.TypeOf(args)
		if p != nil {
			return t, p
		}
		if !ValidToken(typ) {
			return t, &Problem{Path: "args", Message: fmt.Sprintf("%q is not a valid target type", typ)}
		}
		t.Type = typ
	}
	return t, nil
}

// Position is how far a subscription has seen, as a sequence per stream.
type Position map[string]uint64

// Merge is p raised to every position in q.
func (p Position) Merge(q Position) Position {
	out := Position{}
	for k, v := range p {
		out[k] = v
	}
	for k, v := range q {
		if v > out[k] {
			out[k] = v
		}
	}
	return out
}

// Reached reports whether p has seen seq on stream.
func (p Position) Reached(stream string, seq uint64) bool { return p[stream] >= seq }

// CallRun is one running call.
type CallRun struct {
	features.ServiceArgs // Ctx is cancelled by the client's cancel frame
	Args                 json.RawMessage
	// Emit sends one event of the call's stream.
	Emit func(event string, data any)
}

// Call is a request answered with a stream of events; Run returns when the
// stream ends, with a failure's response or nil.
type Call struct {
	extensions.Descriptor
	Title       string
	Description string
	Run         func(c CallRun) *features.ServiceResponse
}

// Conn is what a live connection offers its extensions.
type Conn interface {
	ID() string
	Args() features.ServiceArgs // the connection's principal and context
	Send(frame any)
	// SendBinary sends a binary frame on a channel the extension opened.
	SendBinary(channel uint32, payload []byte)
	// OnBinary routes the connection's binary frames on channel to fn.
	OnBinary(channel uint32, fn func(payload []byte))
	CloseChannel(channel uint32)
}

// Extension handles its own frame types on every connection. Attach is
// called once per connection; the handler gets the raw frame of each type
// it lists, and Close when the connection ends.
type Extension struct {
	extensions.Descriptor
	Frames []string
	Attach func(c Conn) ConnHandler
}

// ConnHandler is an extension's state on one connection.
type ConnHandler interface {
	Handle(frameType string, raw json.RawMessage)
	Close()
}
