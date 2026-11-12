// Package jobs is durable background work on NATS JetStream (forge 07a,
// documentation/Jobs.md): a job runs one published graph (a server.action
// or app.trigger graph) with inputs, as the principal it names, once, and
// is retried with backoff when it fails retryably. An installed app's
// trigger graphs run as jobs: for each event their event node starts on
// (On record changed, On event) and on each tick of On schedule.
//
// It is built for several runners per tenant in several processes on
// several machines sharing one JetStream: every piece of shared state is
// in JetStream and every step is idempotent.
//
//   - A job's state is CQRS like everything else: job.* commands commit
//     events on the tenant's jobs stream (EVT_<tenant>_jobs) and the
//     jobs.job projection folds them; jobs.list reads it. Races between
//     processes (a cancel and a start) are settled by the bus's
//     expected-sequence commits.
//   - Work is dispatched through the work-queue stream JOBS_<tenant>, one
//     durable pull consumer per queue shared by every runner, so each
//     message goes to one runner, which acknowledges it when the run ends;
//     a runner that dies mid-run leaves it unacknowledged and another runs
//     it after the ack wait. Delays and On schedule ticks are JetStream
//     message schedules, fired by the server, so no runner is a leader.
//   - Jobs with one key run one at a time, in order: a keyed job runs only
//     when it is the earliest message left on its key's subject.
//   - A command graph's Enqueue job rides on the commit's first event
//     (live.Event.Jobs) and is enqueued by the area's outbox follower.
package jobs

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("jobs")

// Area is the jobs' area on the live core: EVT_<tenant>_jobs.
const Area live.Area = "jobs"

// TargetType is a job on the jobs stream; its id is the job's.
const TargetType = "job"

// Projector folds each job's state, keyed job.<id>.
const Projector = "jobs.job"

// DefaultQueue is the queue of a job that names none.
const DefaultQueue = "default"

// Statuses a job moves through.
const (
	StatusQueued   = "queued"   // waiting to run (or for its runAt)
	StatusRunning  = "running"  // a runner is running it
	StatusRetrying = "retrying" // failed retryably, waiting for retryAt
	StatusDone     = "done"     // ran to its end
	StatusDead     = "dead"     // failed for good: not retryable, or out of attempts
	StatusCanceled = "canceled" // canceled before it ran
)

// Events the job commands commit.
const (
	EventQueued    = "queued"    // {job}
	EventStarted   = "started"   // {attempt, runner, at}
	EventSucceeded = "succeeded" // {run}
	EventFailed    = "failed"    // {run, retryAt}: to be retried
	EventDied      = "died"      // {run}: failed for good
	EventCanceled  = "canceled"  // {by}
	EventRetried   = "retried"   // {by}: a dead or canceled job queued again
)

// Request is a job as it is asked for (job.enqueue's args).
type Request struct {
	// ID is the job's id and its idempotency key: a second job with an id
	// already enqueued is dropped, however long after.
	ID    string `json:"id"`
	Graph string `json:"graph"`
	// Event is the event node type its graph starts from: On call (a
	// server.action's) when empty; a trigger's event node for a trigger.
	Event string `json:"event,omitempty"`
	// Trigger is the manifest trigger a trigger's job runs for: its graph
	// is the installed version's.
	Trigger string          `json:"trigger,omitempty"`
	Inputs  json.RawMessage `json:"inputs,omitempty"`
	// As is the principal the job runs as.
	As features.PrincipalRef `json:"as"`
	// App is the app whose installed version's copy of Graph runs.
	App   string    `json:"app,omitempty"`
	Queue string    `json:"queue,omitempty"`
	Key   string    `json:"key,omitempty"`
	RunAt time.Time `json:"runAt,omitzero"`
	// MaxAttempts overrides the jobs section's for this job.
	MaxAttempts int `json:"maxAttempts,omitempty"`
	// Request is the request that enqueued it (the command's or round's).
	Request string `json:"request,omitempty"`
}

// Run is one attempt of a job.
type Run struct {
	Attempt    int       `json:"attempt"`
	Runner     string    `json:"runner,omitempty"`
	RunID      uint64    `json:"runId,string,omitempty"` // the Cognition run, whose trace it is
	Status     string    `json:"status"`                 // running, ok, failed, lost
	Error      string    `json:"error,omitempty"`
	FailedNode string    `json:"failedNode,omitempty"`
	Version    int       `json:"version,omitempty"` // the graph's version it ran
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// Run statuses.
const (
	RunRunning = "running"
	RunOK      = "ok"
	RunFailed  = "failed"
	RunLost    = "lost" // its runner stopped mid-run; it ran again
)

// keepRuns is how many attempts a job keeps.
const keepRuns = 20

// Job is a job's state, as jobs.job holds it.
type Job struct {
	Request
	Status     string    `json:"status"`
	Attempts   int       `json:"attempts"`
	EnqueuedAt time.Time `json:"enqueuedAt"`
	StartedAt  time.Time `json:"startedAt,omitzero"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	RetryAt    time.Time `json:"retryAt,omitzero"`
	Error      string    `json:"error,omitempty"`
	Runs       []Run     `json:"runs"`
	// Dispatch counts the job's dispatches: its work message's id is
	// <id>#<dispatch>, so a dispatch sent twice is dropped once.
	Dispatch int `json:"dispatch"`
	// By is who canceled or last retried it.
	By string `json:"by,omitempty"`
}

// Finished reports whether the job will not run again unless retried.
func (j Job) Finished() bool {
	return j.Status == StatusDone || j.Status == StatusDead || j.Status == StatusCanceled
}

// StreamName is the tenant's work-queue stream.
func StreamName(tenant uint64) string { return fmt.Sprintf("JOBS_%d", tenant) }

// Subjects are the work-queue stream's subjects.
func Subjects(tenant uint64) string { return fmt.Sprintf("jobs.%d.>", tenant) }

// RunSubject is where a job's work message goes: its queue and key.
func RunSubject(tenant uint64, queue, key string) string {
	return fmt.Sprintf("jobs.%d.run.%s.%s", tenant, queueOf(queue), KeyToken(key))
}

// AtSubject holds a delayed job's schedule until it is due.
func AtSubject(tenant uint64, id string) string { return fmt.Sprintf("jobs.%d.at.%s", tenant, id) }

// ScheduleSubject holds an installed trigger's On schedule.
func ScheduleSubject(tenant uint64, app, trigger string) string {
	return fmt.Sprintf("jobs.%d.every.%s.%s", tenant, app, trigger)
}

// ConsumerName is a queue's durable consumer, shared by every runner.
func ConsumerName(queue string) string { return "run_" + queueOf(queue) }

func queueOf(q string) string {
	if q == "" {
		return DefaultQueue
	}
	return q
}

// KeyToken is a key as a subject token: _ for none, else a hash of it, so
// any text is a key.
func KeyToken(key string) string {
	if key == "" {
		return "_"
	}
	sum := sha256.Sum256([]byte(key))
	return strings.ToLower(base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:10]))
}

// work is a work message's body: the job it runs, or, for an On schedule
// tick, the trigger it runs for (the job is made from the tick).
type work struct {
	ID   string `json:"id,omitempty"`
	Tick *tick  `json:"tick,omitempty"`
}

type tick struct {
	App     string `json:"app"`
	Trigger string `json:"trigger"`
	Queue   string `json:"queue,omitempty"`
}

// Resource is a job's authz resource: jobs are the tenant's.
func Resource(p features.Principal) string { return fmt.Sprintf("tenant:%d", p.TenantID) }
