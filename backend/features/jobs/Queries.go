package jobs

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/rest/cognitiondto"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// ListParams are jobs.list's params.
type ListParams struct {
	App    string `json:"app,omitempty"`
	Status string `json:"status,omitempty"`
	Graph  string `json:"graph,omitempty"`
}

const listParams = `{"type":"object","properties":{
  "app":{"type":"string","description":"Only the jobs of this app (its triggers' and its graphs')"},
  "status":{"type":"string","enum":["queued","running","retrying","dead","done","canceled"],"description":"Only jobs in this status"},
  "graph":{"type":"string","description":"Only the jobs running this graph"}}}`

// visibility decides which jobs a subscriber sees: their own, every job
// with job.manage, and an app's jobs run as the app when they may read the
// app (its sync state, its last error).
type visibility struct {
	p      features.Principal
	all    bool
	apps   map[string]bool
	params ListParams
	since  time.Time // finished jobs before it are not listed
	check  func(app string) bool
}

func (v *visibility) sees(j Job) bool {
	switch {
	case v.params.App != "" && j.App != v.params.App:
		return false
	case v.params.Status != "" && j.Status != v.params.Status:
		return false
	case v.params.Graph != "" && j.Graph != v.params.Graph:
		return false
	case (j.Status == StatusDone || j.Status == StatusCanceled) && j.FinishedAt.Before(v.since):
		return false
	case v.all || j.As == v.p.Ref():
		return true
	case j.As.Kind == features.PrincipalApp && j.App != "":
		seen, ok := v.apps[j.App]
		if !ok {
			seen = v.check(j.App)
			v.apps[j.App] = seen
		}
		return seen
	}
	return false
}

// visibilityOf is what the subscriber of a jobs query may see.
func visibilityOf(args features.ServiceArgs, d Deps, params ListParams) *visibility {
	s := Defaults
	if d.Config != nil {
		if read, err := config.Read[Settings](d.Config, args, SettingsID); err == nil {
			s = settingsOf(read)
		}
	}
	return &visibility{p: args.Principal, params: params, apps: map[string]bool{}, since: time.Now().Add(-time.Duration(s.DoneHours) * time.Hour),
		all: authz.Check(args.Context(), args.Principal, "job.manage", Resource(args.Principal)).Allowed,
		check: func(app string) bool {
			return authz.Check(args.Context(), args.Principal, "app.read", apps.Resource(args.Principal, app)).Allowed
		}}
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "jobs.list", Source: "jobs"}, Title: "Background jobs",
		Description: "The tenant's background jobs the user may see (their own; an app's jobs when they may read the app; every one with job.manage), keyed job.<id>, each {id, graph, app, trigger, queue, key, as, status (queued, running, retrying, dead, done, canceled), attempts, runAt, enqueuedAt, startedAt, finishedAt, retryAt, error, runs: [{attempt, runId, status, error, failedNode, startedAt, finishedAt}]}; done and canceled jobs for the jobs section's doneHours. Changes as jobs run.",
		Params:      json.RawMessage(listParams), Examples: []json.RawMessage{json.RawMessage(`{"status":"dead"}`)},
		Action: "job.read",
		Run: func(f live.Feed) error {
			var params ListParams
			if raw := f.Params(); len(raw) > 0 {
				if err := json.Unmarshal(raw, &params); err != nil {
					return err
				}
			}
			return list(f, visibilityOf(f.Args(), deps(), params))
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "jobs.trace", Source: "jobs"}, Title: "A job run's trace",
		Description: "The Cognition trace of one attempt of a background job the user may see (as jobs.list decides): {run: {id, graphId, status, error, failedNode, startedAt, durationMs, steps}, steps: [{seq, nodeId, nodeType, inputs, outputs, execPin, durationMs, error}]}. A trace does not change once its run ends.",
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The job's id"},"runId":{"type":"string","description":"The attempt's run id, from the job's runs"}},"required":["id","runId"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"sync-42","runId":"114032675894073755"}`)},
		Action:      "job.read",
		Run: func(f live.Feed) error {
			var p struct {
				ID    string `json:"id"`
				RunID uint64 `json:"runId,string"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil {
				return err
			}
			args := f.Args()
			v, ok, err := f.Store().Get(args.Context(), args.Principal.TenantID, Projector, TargetType+"."+p.ID)
			var j Job
			if err != nil || !ok || json.Unmarshal(v.Value, &j) != nil {
				return log.Code("job_not_found").Errorf("there is no job %s", p.ID)
			}
			vis := visibilityOf(args, deps(), ListParams{})
			vis.since = time.Time{}
			if !vis.sees(j) || !slices.ContainsFunc(j.Runs, func(r Run) bool { return r.RunID == p.RunID }) {
				return log.Code("job_not_found").Errorf("there is no run %d of a job %s you may see", p.RunID, p.ID)
			}
			d := deps()
			if d.Engine == nil {
				return log.Errorf("there is no engine")
			}
			// Traces are kept where the run ran: this server's database
			var run cognition.Run
			if err := d.Engine.DB().WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).First(&run, p.RunID).Error; err != nil {
				return log.Code("run_not_found").Errorf("the trace of run %d is not kept here; it may have expired", p.RunID)
			}
			var steps []cognition.Step
			if err := d.Engine.DB().WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).Where("run_id = ?", run.ID).Order("seq").Find(&steps).Error; err != nil {
				return log.Wrap(err)
			}
			// As /cognition/trace gives it
			trace := cognitiondto.GetTraceResponseFrom(cognitionservice.GetTraceResponse{Common: features.OK(), Run: run, Steps: steps})
			f.Snapshot(map[string]any{"run": trace.Run, "steps": trace.Steps}, live.Position{})
			<-f.Context().Done()
			return nil
		},
	})
}

// list sends the jobs v sees, then their changes; a job leaving what v
// sees is removed.
func list(f live.Feed, v *visibility) error {
	entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, Projector, TargetType+".*")
	if err != nil {
		return err
	}
	initial := map[string]json.RawMessage{}
	shown := map[string]bool{}
	pos := live.Position{}
	started := false
	for e := range entries {
		if e == nil {
			started = true
			f.Snapshot(initial, pos)
			continue
		}
		pos = pos.Merge(e.Value.Position())
		var j Job
		visible := !e.Deleted && json.Unmarshal(e.Value.Value, &j) == nil && v.sees(j)
		switch {
		case !started && visible:
			initial[e.Key], shown[e.Key] = e.Value.Value, true
		case !started:
			delete(initial, e.Key)
			delete(shown, e.Key)
		case visible:
			shown[e.Key] = true
			f.Change(live.Delta{Op: "upsert", Key: e.Key, Value: e.Value.Value}, pos)
		case shown[e.Key]:
			delete(shown, e.Key)
			f.Change(live.Delta{Op: "remove", Key: e.Key}, pos)
		default:
			f.Change(live.Reached, pos)
		}
	}
	return nil
}
