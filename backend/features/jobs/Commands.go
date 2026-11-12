package jobs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

const requestSchema = `{"type":"object","properties":{
  "id":{"type":"string","description":"The job's id, which is also its idempotency key"},
  "graph":{"type":"string","description":"The published server.action or app.trigger graph it runs"},
  "event":{"type":"string","description":"The event node type its graph starts from; On call when left out"},
  "trigger":{"type":"string","description":"The manifest trigger it runs for"},
  "inputs":{"type":"object","description":"The graph's inputs"},
  "as":{"type":"object","description":"The principal it runs as: {kind, userId, app}"},
  "app":{"type":"string","description":"The app whose installed version's copy of the graph runs"},
  "queue":{"type":"string","description":"Its queue; default when left out"},
  "key":{"type":"string","description":"Jobs with one key run one at a time, in order"},
  "runAt":{"type":"string","description":"When it may run, RFC 3339; now when left out"},
  "maxAttempts":{"type":"integer","description":"How many times it runs before it is dead, in place of the jobs section's"},
  "request":{"type":"string","description":"The request that enqueued it"}},"required":["id","graph","as"]}`

type startArgs struct {
	Runner string `json:"runner"`
	// Job makes the job when there is none yet: an On schedule tick.
	Job *Request `json:"job,omitempty"`
}

type finishArgs struct {
	Attempt    int       `json:"attempt"`
	Outcome    string    `json:"outcome"` // done, retry or dead
	RunID      uint64    `json:"runId,string,omitempty"`
	Version    int       `json:"version,omitempty"`
	Error      string    `json:"error,omitempty"`
	FailedNode string    `json:"failedNode,omitempty"`
	RetryAt    time.Time `json:"retryAt,omitzero"`
}

type change struct {
	Job     *Job      `json:"job,omitempty"`
	Attempt int       `json:"attempt,omitempty"`
	Runner  string    `json:"runner,omitempty"`
	At      time.Time `json:"at,omitzero"`
	Run     *Run      `json:"run,omitempty"`
	RetryAt time.Time `json:"retryAt,omitzero"`
	By      string    `json:"by,omitempty"`
}

func problem(path, message, fix string) []live.Problem {
	return []live.Problem{{Path: path, Message: message, Fix: fix}}
}

// Outcomes of a finished run.
const (
	OutcomeDone  = "done"
	OutcomeRetry = "retry"
	OutcomeDead  = "dead"
)

// checkRequest refuses a job that cannot be enqueued.
func checkRequest(p features.Principal, r Request) []live.Problem {
	switch {
	case !live.ValidToken(r.ID):
		return problem("args.id", fmt.Sprintf("%q is not a job id", r.ID), "use letters, digits, - and _")
	case !live.ValidToken(r.Graph):
		return problem("args.graph", fmt.Sprintf("%q is not a graph id", r.Graph), "")
	case r.Queue != "" && !live.ValidToken(r.Queue):
		return problem("args.queue", fmt.Sprintf("%q is not a queue name", r.Queue), "use letters, digits, - and _")
	case r.App != "" && !live.ValidToken(r.App):
		return problem("args.app", fmt.Sprintf("%q is not an app id", r.App), "")
	case r.As.Kind == features.PrincipalAnonymous || r.As.Kind == "":
		return problem("args.as", "a job runs as a principal", "")
	case r.As.Kind == features.PrincipalApp && r.As.App == "":
		return problem("args.as.app", "an app principal names its app", "")
	}
	// A job runs as whoever enqueues it; only the server's own work (the
	// outbox, triggers) enqueues for another principal
	if p.Kind != features.PrincipalService && r.As != p.Ref() {
		return problem("args.as", "a job runs as the principal who enqueues it", "leave as to the server")
	}
	return nil
}

// mayAct reports whether p may cancel or retry the job: its own, or with
// job.manage, or the server.
func mayAct(d live.Decision, j Job) bool {
	p := d.Principal
	if p.Kind == features.PrincipalService || j.As == p.Ref() {
		return true
	}
	return authz.Check(d.Context(), p, "job.manage", Resource(p)).Allowed
}

type spec struct {
	id, title, description, args, example, action string
	internal                                      bool
	events                                        []string
	decide                                        func(d live.Decision, j *Job) ([]live.NewEvent, []live.Problem)
}

func specs() []spec {
	return []spec{
		{
			id: "job.enqueue", title: "Enqueue a job", internal: true, action: "job.create", events: []string{EventQueued}, args: requestSchema,
			example:     `{"id":"sync-42","graph":"mail-sync","as":{"kind":"user","userId":"1"},"key":"account-42"}`,
			description: "Enqueues a background job that runs a published graph as the principal who enqueued it; a job whose id is already enqueued is dropped. Sent by the Enqueue job node and the server, not as a tool.",
			decide: func(d live.Decision, j *Job) ([]live.NewEvent, []live.Problem) {
				if j != nil {
					return nil, nil // enqueued before: the id is its idempotency key
				}
				var r Request
				if err := json.Unmarshal(d.Args, &r); err != nil {
					return nil, problem("args", err.Error(), "")
				}
				r.ID = d.Target.ID
				if ps := checkRequest(d.Principal, r); ps != nil {
					return nil, ps
				}
				r.Queue = queueOf(r.Queue)
				return []live.NewEvent{{Type: EventQueued, Data: change{Job: &Job{Request: r, Status: StatusQueued, EnqueuedAt: time.Now().UTC(), Runs: []Run{}, Dispatch: 1}}}}, nil
			},
		},
		{
			id: "job.start", title: "Start a job's run", internal: true, action: "job.manage", events: []string{EventQueued, EventStarted},
			args:        `{"type":"object","properties":{"runner":{"type":"string","description":"The runner starting it"},"job":{"type":"object","description":"The job to make first, for an On schedule tick"}},"required":["runner"]}`,
			example:     `{"runner":"host-1234"}`,
			description: "A runner starts a job's next attempt; refused when the job was canceled or has finished, so it is not run. Sent by the runners only.",
			decide: func(d live.Decision, j *Job) ([]live.NewEvent, []live.Problem) {
				if d.Principal.Kind != features.PrincipalService {
					return nil, problem("", "only the runners start jobs", "")
				}
				var a startArgs
				if err := json.Unmarshal(d.Args, &a); err != nil {
					return nil, problem("args", err.Error(), "")
				}
				var events []live.NewEvent
				if j == nil {
					if a.Job == nil {
						return nil, []live.Problem{{Code: "job_not_found", Message: fmt.Sprintf("there is no job %s", d.Target.ID)}}
					}
					r := *a.Job
					r.ID, r.Queue = d.Target.ID, queueOf(r.Queue)
					if ps := checkRequest(d.Principal, r); ps != nil {
						return nil, ps
					}
					j = &Job{Request: r, Status: StatusQueued, EnqueuedAt: time.Now().UTC(), Runs: []Run{}, Dispatch: 1}
					events = append(events, live.NewEvent{Type: EventQueued, Data: change{Job: j}})
				}
				if j.Finished() {
					return nil, []live.Problem{{Code: "job_finished", Message: fmt.Sprintf("the job %s is %s", j.ID, j.Status)}}
				}
				return append(events, live.NewEvent{Type: EventStarted, Data: change{Attempt: j.Attempts + 1, Runner: a.Runner, At: time.Now().UTC()}}), nil
			},
		},
		{
			id: "job.finish", title: "Finish a job's run", internal: true, action: "job.manage", events: []string{EventSucceeded, EventFailed, EventDied},
			args:        `{"type":"object","properties":{"attempt":{"type":"integer","description":"The attempt that ended"},"outcome":{"type":"string","enum":["done","retry","dead"],"description":"How it ended"},"runId":{"type":"string","description":"Its Cognition run"},"version":{"type":"integer","description":"The graph version it ran"},"error":{"type":"string","description":"Why it failed"},"failedNode":{"type":"string","description":"The node it failed on"},"retryAt":{"type":"string","description":"When to try again, for retry"}},"required":["attempt","outcome"]}`,
			example:     `{"attempt":1,"outcome":"done"}`,
			description: "A runner records how a job's attempt ended: done, to be retried at retryAt, or dead. An attempt that is not the job's current one changes nothing. Sent by the runners only.",
			decide: func(d live.Decision, j *Job) ([]live.NewEvent, []live.Problem) {
				if d.Principal.Kind != features.PrincipalService {
					return nil, problem("", "only the runners finish jobs", "")
				}
				var a finishArgs
				if err := json.Unmarshal(d.Args, &a); err != nil {
					return nil, problem("args", err.Error(), "")
				}
				if j == nil || j.Status != StatusRunning || j.Attempts != a.Attempt {
					return nil, nil // a run that is not current: another runner took it over
				}
				run := lastRun(j)
				run.Status, run.RunID, run.Version, run.Error, run.FailedNode, run.FinishedAt = RunOK, a.RunID, a.Version, a.Error, a.FailedNode, time.Now().UTC()
				switch a.Outcome {
				case OutcomeDone:
					return []live.NewEvent{{Type: EventSucceeded, Data: change{Run: &run}}}, nil
				case OutcomeRetry:
					run.Status = RunFailed
					return []live.NewEvent{{Type: EventFailed, Data: change{Run: &run, RetryAt: a.RetryAt}}}, nil
				case OutcomeDead:
					run.Status = RunFailed
					return []live.NewEvent{{Type: EventDied, Data: change{Run: &run}}}, nil
				}
				return nil, problem("args.outcome", fmt.Sprintf("%q is not an outcome", a.Outcome), "done, retry or dead")
			},
		},
		{
			id: "job.cancel", title: "Cancel a job", action: "job.create", events: []string{EventCanceled}, args: `{"type":"object","properties":{}}`, example: `{}`,
			description: "Cancels a background job (the target is its id) that has not started: queued, delayed or waiting to be retried. A running or finished job cannot be canceled. Your own jobs, or any with job.manage.",
			decide: func(d live.Decision, j *Job) ([]live.NewEvent, []live.Problem) {
				if j == nil {
					return nil, []live.Problem{{Code: "job_not_found", Path: "target", Message: fmt.Sprintf("there is no job %s", d.Target.ID), Fix: "list jobs with jobs.list"}}
				}
				if !mayAct(d, *j) {
					return nil, []live.Problem{{Code: "access_denied", Path: "target", Message: "the job is someone else's", Fix: "job.manage lets you act on everyone's jobs"}}
				}
				if j.Status != StatusQueued && j.Status != StatusRetrying {
					return nil, []live.Problem{{Code: "job_started", Path: "target", Message: fmt.Sprintf("the job %s is %s, so it cannot be canceled", j.ID, j.Status)}}
				}
				return []live.NewEvent{{Type: EventCanceled, Data: change{By: d.Principal.String()}}}, nil
			},
		},
		{
			id: "job.retry", title: "Retry a job", action: "job.create", events: []string{EventRetried}, args: `{"type":"object","properties":{}}`, example: `{}`,
			description: "Queues a dead or canceled background job (the target is its id) again, with its attempts counted afresh. Your own jobs, or any with job.manage.",
			decide: func(d live.Decision, j *Job) ([]live.NewEvent, []live.Problem) {
				if j == nil {
					return nil, []live.Problem{{Code: "job_not_found", Path: "target", Message: fmt.Sprintf("there is no job %s", d.Target.ID), Fix: "list jobs with jobs.list"}}
				}
				if !mayAct(d, *j) {
					return nil, []live.Problem{{Code: "access_denied", Path: "target", Message: "the job is someone else's", Fix: "job.manage lets you act on everyone's jobs"}}
				}
				if j.Status != StatusDead && j.Status != StatusCanceled {
					return nil, []live.Problem{{Code: "job_not_finished", Path: "target", Message: fmt.Sprintf("the job %s is %s; only a dead or canceled job is retried", j.ID, j.Status)}}
				}
				return []live.NewEvent{{Type: EventRetried, Data: change{By: d.Principal.String()}}}, nil
			},
		},
	}
}

// lastRun is the job's current attempt's run.
func lastRun(j *Job) Run {
	if n := len(j.Runs); n > 0 && j.Runs[n-1].Attempt == j.Attempts {
		return j.Runs[n-1]
	}
	return Run{Attempt: j.Attempts, StartedAt: j.StartedAt}
}

// Contribute registers the jobs projector, commands and query; deps
// returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: "jobs"}, Title: "Background jobs",
		Description: "Each background job: {id, graph, event, trigger, inputs, as, app, queue, key, runAt, status, attempts, enqueuedAt, startedAt, finishedAt, retryAt, error, runs: [{attempt, runner, runId, status, error, failedNode, startedAt, finishedAt}]}",
		Area:        Area, Types: []string{TargetType}, Fold: fold,
	})
	for _, s := range specs() {
		live.Commands.Register(reg, live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "jobs"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events, Internal: s.internal,
			Area: Area, TargetType: TargetType, Projection: Projector, Action: s.action,
			Resource: func(p features.Principal, _ live.Target) string { return Resource(p) },
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				var j *Job
				if d.State != nil {
					j = &Job{}
					if err := json.Unmarshal(d.State, j); err != nil {
						return nil, problem("", "the job cannot be read: "+err.Error(), "")
					}
				}
				return s.decide(d, j)
			},
		})
	}
	contributeQueries(reg, deps)
}

// fold is the jobs.job projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	var j Job
	if f.State != nil {
		if err := json.Unmarshal(f.State, &j); err != nil {
			return nil, err
		}
	}
	var c change
	if err := json.Unmarshal(f.Event.Data, &c); err != nil {
		return nil, err
	}
	e := f.Event
	switch e.Type {
	case EventQueued:
		j = *c.Job
	case EventStarted:
		if n := len(j.Runs); n > 0 && j.Runs[n-1].Status == RunRunning {
			j.Runs[n-1].Status, j.Runs[n-1].FinishedAt = RunLost, c.At // its runner stopped mid-run
		}
		j.Status, j.Attempts, j.StartedAt, j.RetryAt = StatusRunning, c.Attempt, c.At, time.Time{}
		j.Runs = append(j.Runs, Run{Attempt: c.Attempt, Runner: c.Runner, Status: RunRunning, StartedAt: c.At})
	case EventSucceeded, EventFailed, EventDied:
		if n := len(j.Runs); n > 0 && c.Run != nil && j.Runs[n-1].Attempt == c.Run.Attempt {
			j.Runs[n-1] = *c.Run
		}
		j.Error = ""
		if c.Run != nil {
			j.Error = c.Run.Error
		}
		switch e.Type {
		case EventSucceeded:
			j.Status, j.FinishedAt = StatusDone, e.Time
		case EventFailed:
			j.Status, j.RetryAt = StatusRetrying, c.RetryAt
		case EventDied:
			j.Status, j.FinishedAt = StatusDead, e.Time
		}
	case EventCanceled:
		j.Status, j.FinishedAt, j.By = StatusCanceled, e.Time, c.By
	case EventRetried:
		j.Status, j.Attempts, j.Error, j.FinishedAt, j.RetryAt, j.By = StatusQueued, 0, "", time.Time{}, time.Time{}, c.By
		j.RunAt = time.Time{}
		j.Dispatch++
	default:
		return f.State, nil
	}
	if len(j.Runs) > keepRuns {
		j.Runs = j.Runs[len(j.Runs)-keepRuns:]
	}
	return json.Marshal(j)
}
