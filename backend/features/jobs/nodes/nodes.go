// Package jobsnodes is the jobs' Cognition nodes (forge 07a): Enqueue job
// and Cancel job, Fail, which ends a job's run as retryable or not, and
// Job info.
package jobsnodes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
)

const source = "jobs"

func node(reg *extensions.Registry, order int, t cognition.NodeType) {
	t.Order, t.Source = order, source
	if t.Category == "" {
		t.Category = "Jobs"
	}
	cognition.Nodes.Register(reg, t)
}

// Deps are what the nodes need, made after the registry.
type Deps struct {
	Runner *jobs.Runner
	Store  live.Store // the installs, for an app's graphs
}

// Contribute registers the nodes; deps returns their dependencies once
// made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	node(reg, 900, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "jobs.enqueue"}, Title: "Enqueue job", Effects: true, PureIn: []string{livenodes.KindCommand},
		Kinds:       []string{livenodes.KindCommand, livenodes.KindAction, livenodes.KindTrigger, livenodes.KindConnectorOp, "ui.handler"},
		Description: "Runs a server.action graph later, in the background, as the run's principal, as a durable job: retried when it fails retryably, run once even if a server dies. In a command graph it is enqueued only if the command's events commit. Jobs with one key run one at a time, in order; a job's id makes it run once however often it is enqueued.",
		Example:     "Enqueue job mail-send with the draft's id as inputs, key the account and a 10s delay, so Undo send can cancel it",
		Examples:    cognition.ConfigExample("jobs.enqueue", `{"graph":"mail-send"}`),
		Config: json.RawMessage(`{"type":"object","properties":{
			"graph":{"type":"string","title":"Graph","description":"The server.action graph the job runs, published (an app's own graphs run from its installed version)"},
			"queue":{"type":"string","title":"Queue","description":"The queue it waits in; default when empty"}},"required":["graph"]}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				{ID: "inputs", Name: "Inputs", Type: cognition.Any, Optional: true, Description: "The graph's inputs, an object of its On call outputs by name"},
				{ID: "key", Name: "Key", Type: "string", Optional: true, Description: "Jobs with the same key run one at a time, in order"},
				{ID: "delay", Name: "Delay", Type: "string", Optional: true, Description: "How long to wait before it runs, such as 10s or 5m"},
				{ID: "runAt", Name: "Run at", Type: "string", Optional: true, Description: "When it runs, RFC 3339, in place of a delay"},
				{ID: "id", Name: "ID", Type: "string", Optional: true, Description: "The job's id: a job with an id already enqueued is not enqueued again; a new one when empty"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("id", "ID", "string", "The job's id, for Cancel job")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Graph, Queue string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			d := deps()
			if d.Runner == nil {
				return nil, "", fmt.Errorf("there is no job runner")
			}
			id, _ := cognition.Get[string](in, "id")
			if id == "" {
				id = "job-" + strconv.FormatUint(features.NewTSID(), 10)
			}
			if !live.ValidToken(id) {
				return nil, "", fmt.Errorf("%q is not a job id: use letters, digits, - and _", id)
			}
			var runAt time.Time
			if s, _ := cognition.Get[string](in, "runAt"); s != "" {
				t, err := time.Parse(time.RFC3339, s)
				if err != nil {
					return nil, "", fmt.Errorf("run at %q is not an RFC 3339 time", s)
				}
				runAt = t.UTC()
			}
			if s := fmt.Sprint(in["delay"]); in["delay"] != nil && s != "" {
				delay, err := time.ParseDuration(s)
				if err != nil {
					if secs, perr := strconv.ParseFloat(s, 64); perr == nil {
						delay, err = time.Duration(secs*float64(time.Second)), nil
					}
				}
				if err != nil {
					return nil, "", fmt.Errorf("delay %q is not a duration such as 10s or 5m", s)
				}
				runAt = time.Now().Add(delay).UTC()
			}
			key, _ := cognition.Get[string](in, "key")
			var inputs json.RawMessage
			if v := in["inputs"]; v != nil {
				inputs, _ = json.Marshal(v)
			}
			app := appOf(x, d, c.Graph)
			// In a command graph the job rides on the commit (the outbox)
			if h, ok := x.Host.(*livenodes.CommandHost); ok && h.Decision != nil {
				if bus.DryOf(x.Context()) == nil {
					if err := d.Runner.Outbox(x.ServiceArgs, h.Decision.Target.Area); err != nil {
						return nil, "", err
					}
				}
				h.Decision.Enqueue(live.Job{ID: id, Graph: c.Graph, Inputs: inputs, Queue: c.Queue, Key: key, RunAt: runAt, App: app, As: x.Principal.Ref()})
				return cognition.Values{"id": id}, cognition.Then, nil
			}
			j, err := d.Runner.Enqueue(x.ServiceArgs, jobs.Request{ID: id, Graph: c.Graph, Inputs: inputs, Queue: c.Queue, Key: key, RunAt: runAt, App: app})
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"id": j.ID}, cognition.Then, nil
		},
	})
	node(reg, 910, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "jobs.cancel"}, Title: "Cancel job", Effects: true,
		Kinds:       []string{livenodes.KindAction, livenodes.KindTrigger, livenodes.KindConnectorOp, "ui.handler"},
		Description: "Cancels a job that has not started yet, as the run's principal (Undo send); continues by Canceled, or Not canceled when it is running, finished or someone else's",
		Example:     "Cancel job with the id Enqueue job gave when the user clicks Undo send",
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("id", "ID", "string", "The job's id")},
			Out: []cognition.Pin{cognition.ExecPin("canceled", "Canceled"), cognition.ExecPin("failed", "Not canceled"),
				cognition.DataPin("message", "Message", "string", "Why it was not canceled")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			d := deps()
			if d.Runner == nil {
				return nil, "", fmt.Errorf("there is no job runner")
			}
			id, _ := cognition.Get[string](in, "id")
			if err := d.Runner.Cancel(x.ServiceArgs, id); err != nil {
				return cognition.Values{"message": err.Error()}, "failed", nil
			}
			return cognition.Values{"message": ""}, "canceled", nil
		},
	})
	node(reg, 920, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "jobs.fail"}, Title: "Fail", Category: "Flow",
		Kinds:       []string{livenodes.KindAction, livenodes.KindTrigger},
		Description: "Ends the run as failed with a message. In a job, a retryable failure runs the job again later (after the given wait, else with backoff) until it runs out of attempts; any other failure makes it dead at once",
		Example:     "Fail retryable \"the mail server is busy\" after 1m, when the connector answers rateLimited",
		Config:      json.RawMessage(`{"type":"object","properties":{"retryable":{"type":"boolean","title":"Retryable","description":"Whether trying again later may succeed"}}}`),
		Pins: cognition.Pins{In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
			cognition.DataPin("message", "Message", "string", "Why it failed, in words a person can act on"),
			{ID: "after", Name: "After", Type: "string", Optional: true, Description: "How long to wait before retrying, such as 30s; the backoff's wait when empty"}}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Retryable bool }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			msg, _ := cognition.Get[string](in, "message")
			if strings.TrimSpace(msg) == "" {
				msg = "the graph failed"
			}
			f := &jobs.Failure{Message: msg, Retryable: c.Retryable}
			if s, _ := cognition.Get[string](in, "after"); s != "" {
				after, err := time.ParseDuration(s)
				if err != nil {
					return nil, "", fmt.Errorf("after %q is not a duration such as 30s", s)
				}
				f.After = after
			}
			return nil, "", f
		},
	})
	node(reg, 930, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "jobs.info"}, Title: "Job info", Category: "Data", Volatile: true,
		Kinds:       []string{livenodes.KindAction, livenodes.KindTrigger},
		Description: "The job the run is: its id (unique, so a command keyed by it commits once however often the job runs), its attempt from 1, and the event or tick that started a trigger; empty outside a job",
		Example:     "Job info's id as the target of record.create, so a run that is retried makes one record",
		Pins: cognition.Pins{Out: []cognition.Pin{
			cognition.DataPin("id", "ID", "string", "The job's id; empty outside a job"),
			cognition.DataPin("attempt", "Attempt", "number", "Which attempt this run is, from 1"),
			cognition.DataPin("event", "Event", "json", "The event or tick that started the trigger"),
			cognition.DataPin("app", "App", "string", "The app whose job it is"),
			cognition.DataPin("trigger", "Trigger", "string", "The manifest trigger it runs for")}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			i, _ := jobs.InfoOf(x.Context())
			return cognition.Values{"id": i.ID, "attempt": i.Attempt, "event": i.Event, "app": i.App, "trigger": i.Trigger}, "", nil
		},
	})
}

// appOf is the app whose installed copy of graph a job of the running
// graph runs: the running graph's app, when it is installed and its
// version holds graph; else none, and the tenant's published graph runs.
func appOf(x *cognition.Exec, d Deps, graph string) string {
	app, ok := strings.CutPrefix(x.Owner, "app:")
	if !ok || d.Store == nil {
		return ""
	}
	in, ok := installs.Running(x.Context(), d.Store, x.Principal.TenantID, app)
	if !ok {
		return ""
	}
	if _, ok := in.Version.Graphs[graph]; !ok {
		return ""
	}
	return app
}
