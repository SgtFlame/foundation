package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// installed is an app's install when it is installed.
func installed(ctx context.Context, store live.Store, tenant uint64, app string) (apps.Install, bool) {
	return installs.Running(ctx, store, tenant, app)
}

// triggerEvent is the trigger event of an event node type.
func triggerEvent(e *engine.Engine, nodeType string) (livenodes.TriggerEvent, bool) {
	return livenodes.TriggerEventOf(e.Registry(), nodeType)
}

// TriggerConsumer is the durable consumer of an installed trigger, on its
// event stream: one per install and trigger, shared by every runner, so a
// trigger sees each event once across restarts and processes.
func TriggerConsumer(app, trigger string) string { return "trg_" + app + "_" + trigger }

// started is a trigger's event node: its type and config.
type started struct {
	node   string
	config json.RawMessage
}

// eventNode is a trigger graph's event node.
func eventNode(e *engine.Engine, g cognition.Graph) (started, bool) {
	res := e.Resolve(g)
	for _, n := range g.Nodes {
		if t := res.Types[n.ID]; t.Event {
			return started{node: n.Type, config: n.Config}, true
		}
	}
	return started{}, false
}

// watchInstalls keeps the tenant's triggers in step with its installs:
// each install, upgrade and uninstall, seen by every runner, makes the
// same consumers and schedules, so it does not matter which runs first.
func (r *Runner) watchInstalls(t *tenant) {
	entries, err := r.d.Projections.Watch(r.ctx, t.id, installs.Projector, installs.TargetType+".*")
	if err != nil {
		log.Error(r.ctx, err, "jobs: cannot watch the installs of tenant %d", t.id)
		return
	}
	for e := range entries {
		if e == nil {
			continue
		}
		var in apps.Install
		if e.Deleted || json.Unmarshal(e.Value.Value, &in) != nil {
			continue
		}
		if err := r.reconcile(t, in); err != nil {
			log.With("appId", in.App).Error(r.ctx, err, "jobs: cannot keep the triggers of app %s", in.App)
		}
	}
}

// desired is one installed trigger's consumer or schedule.
type desired struct {
	trigger  apps.Trigger
	graph    cognition.Graph
	node     started
	area     live.Area // for an event trigger
	stream   jetstream.Stream
	subjects []string
	schedule string // for On schedule: the Nats-Schedule pattern
	zone     string
}

// reconcile makes an app's trigger consumers and schedules what its
// install says: none once uninstalled.
func (r *Runner) reconcile(t *tenant, in apps.Install) error {
	app := in.App
	as := features.ServiceArgs{Ctx: r.ctx, Principal: features.AppPrincipal(t.id, app)}
	want := map[string]desired{}
	blocked := false
	// warn logs why a trigger cannot run, once while the reason stays
	warn := func(id string, err error) {
		blocked = true
		key := app + "/" + id
		t.mu.Lock()
		same := t.warned[key] == err.Error()
		t.warned[key] = err.Error()
		t.mu.Unlock()
		if !same {
			log.With("appId", app, "trigger", id).Warn(r.ctx, "jobs: the trigger %s of %s cannot run yet: %v", id, app, err)
		}
	}
	defer func() {
		t.mu.Lock()
		if blocked && in.Installed() {
			t.pending[app] = true
		} else {
			delete(t.pending, app)
		}
		for id := range want {
			delete(t.warned, app+"/"+id)
		}
		t.mu.Unlock()
	}()
	if in.Installed() {
		for _, tr := range in.Version.Manifest.Triggers {
			_, g, ok := in.Version.Trigger(tr.ID)
			if !ok {
				continue
			}
			node, ok := eventNode(r.d.Engine, g)
			if !ok {
				log.With("appId", app, "trigger", tr.ID).Warn(r.ctx, "jobs: the trigger %s of %s has no event node", tr.ID, app)
				continue
			}
			d := desired{trigger: tr, graph: g, node: node}
			switch node.node {
			case livenodes.OnSchedule:
				pattern, zone, err := schedulePattern(node.config)
				if err != nil {
					warn(tr.ID, err)
					continue
				}
				d.schedule, d.zone = pattern, zone
			case livenodes.OnWebhook:
				continue // webhooks come with forge 11-Zapier
			default:
				te, ok := triggerEvent(r.d.Engine, node.node)
				if !ok {
					continue
				}
				area, subjects, err := te.Subjects(as, app, node.config)
				if err != nil {
					warn(tr.ID, err) // perhaps its app's roles are not synced yet
					continue
				}
				d.area, d.subjects = area, subjects
				if te.Stream != nil {
					d.stream, err = te.Stream(r.ctx, r.d.Projections.JetStream(), t.id)
				} else {
					d.stream, err = r.d.Projections.Stream(r.ctx, t.id, area)
				}
				if err != nil {
					return err
				}
			}
			want[tr.ID] = d
		}
	}
	// Schedules: one message schedule per trigger, replaced by the next
	schedules, err := r.schedulesOf(t, app)
	if err != nil {
		return err
	}
	for id, subject := range schedules {
		if d, ok := want[id]; !ok || d.schedule == "" {
			if err := t.stream.Purge(r.ctx, jetstream.WithPurgeSubject(subject)); err != nil {
				return err
			}
		}
	}
	// Event consumers: on every area stream the app's triggers read
	have, err := r.triggerConsumers(t, app)
	if err != nil {
		return err
	}
	for key, stream := range have {
		sname := stream.CachedInfo().Config.Name
		name := strings.TrimPrefix(key, sname+"/")
		if d, ok := want[strings.TrimPrefix(name, TriggerConsumer(app, ""))]; ok && d.stream != nil && d.stream.CachedInfo().Config.Name == sname && name == TriggerConsumer(app, d.trigger.ID) {
			continue
		}
		r.stopTrigger(t, key)
		if err := stream.DeleteConsumer(r.ctx, name); err != nil && !errors.Is(err, jetstream.ErrConsumerNotFound) {
			return err
		}
	}
	for _, id := range slices.Sorted(maps.Keys(want)) {
		d := want[id]
		var err error
		if d.schedule != "" {
			err = r.schedule(t, in, d)
		} else {
			err = r.consume(t, in, d)
		}
		if err != nil {
			log.With("appId", app, "trigger", id).Error(r.ctx, err, "jobs: cannot start the trigger %s of %s", id, app)
		}
	}
	return nil
}

// schedulesOf are the app's schedule subjects in the work queue, by
// trigger.
func (r *Runner) schedulesOf(t *tenant, app string) (map[string]string, error) {
	prefix := ScheduleSubject(t.id, app, "")
	info, err := t.stream.Info(r.ctx, jetstream.WithSubjectFilter(prefix+"*"))
	if err != nil {
		return nil, log.Wrap(err)
	}
	out := map[string]string{}
	for subject := range info.State.Subjects {
		out[strings.TrimPrefix(subject, prefix)] = subject
	}
	return out, nil
}

// triggerConsumers are the app's trigger consumers made on any of the
// tenant's event streams, and on the streams of trigger events that have
// their own (CX_<tenant>), keyed <stream>/<name>.
func (r *Runner) triggerConsumers(t *tenant, app string) (map[string]jetstream.Stream, error) {
	out := map[string]jetstream.Stream{}
	var streams []jetstream.Stream
	prefix := fmt.Sprintf("EVT_%d_", t.id)
	names := r.d.Projections.JetStream().StreamNames(r.ctx, jetstream.WithStreamListSubject(fmt.Sprintf("evt.%d.>", t.id)))
	for name := range names.Name() {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		s, err := r.d.Projections.Stream(r.ctx, t.id, live.Area(strings.TrimPrefix(name, prefix)))
		if err != nil {
			return nil, err
		}
		streams = append(streams, s)
	}
	if err := names.Err(); err != nil {
		return nil, log.Wrap(err)
	}
	for _, te := range livenodes.TriggerEvents.All(r.d.Engine.Registry()) {
		if te.Stream == nil {
			continue
		}
		s, err := te.Stream(r.ctx, r.d.Projections.JetStream(), t.id)
		if err != nil {
			return nil, err
		}
		streams = append(streams, s)
	}
	for _, s := range streams {
		sname := s.CachedInfo().Config.Name
		list := s.ListConsumers(r.ctx)
		for c := range list.Info() {
			if c.Config.Metadata["app"] == app && c.Config.Metadata["trigger"] != "" {
				out[sname+"/"+c.Name] = s
			}
		}
		if err := list.Err(); err != nil {
			return nil, log.Wrap(err)
		}
	}
	return out, nil
}

// consume makes sure an event trigger's consumer exists with its subjects
// and this runner consumes it. A new consumer starts at the install or
// upgrade that made it; one made already keeps its position (an upgrade
// resumes where the old version stopped).
func (r *Runner) consume(t *tenant, in apps.Install, d desired) error {
	name := TriggerConsumer(in.App, d.trigger.ID)
	stream := d.stream
	key := stream.CachedInfo().Config.Name + "/" + name
	cons, err := stream.Consumer(r.ctx, name)
	switch {
	case errors.Is(err, jetstream.ErrConsumerNotFound):
		start := in.UpdatedAt
		cons, err = stream.CreateConsumer(r.ctx, jetstream.ConsumerConfig{
			Durable: name, FilterSubjects: d.subjects, DeliverPolicy: jetstream.DeliverByStartTimePolicy, OptStartTime: &start,
			AckPolicy: jetstream.AckExplicitPolicy, MaxAckPending: 1, AckWait: 30 * time.Second, MaxDeliver: -1,
			Metadata: map[string]string{"app": in.App, "trigger": d.trigger.ID},
		})
		if errors.Is(err, jetstream.ErrConsumerExists) {
			cons, err = stream.Consumer(r.ctx, name)
		}
	case err == nil && !slices.Equal(cons.CachedInfo().Config.FilterSubjects, d.subjects):
		cfg := cons.CachedInfo().Config
		cfg.FilterSubjects = d.subjects
		cons, err = stream.UpdateConsumer(r.ctx, cfg)
	}
	if err != nil {
		return log.Wrapf(err, "cannot make the consumer %s", name)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.triggers[key] != nil || r.closing.Load() {
		return nil
	}
	app, trigger, graph, node, queue := in.App, d.trigger.ID, d.graph.ID, d.node.node, d.trigger.Queue
	var accept func(live.Event) bool
	if te, ok := triggerEvent(r.d.Engine, node); ok && te.Accept != nil {
		config := d.node.config
		accept = func(e live.Event) bool {
			return te.Accept(features.ServiceArgs{Ctx: r.ctx, Principal: features.AppPrincipal(t.id, app)}, config, e)
		}
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) { r.onEvent(t, app, trigger, graph, node, queue, accept, msg) })
	if err != nil {
		return log.Wrapf(err, "cannot consume %s", name)
	}
	t.triggers[key] = cc
	return nil
}

func (r *Runner) stopTrigger(t *tenant, key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.triggers[key]; c != nil {
		c.Stop()
		delete(t.triggers, key)
	}
}

// onEvent enqueues a trigger's job for one event, then acknowledges it:
// the job's id is the event's, so an event delivered twice is one job.
// A trigger does not start on events its own app committed, so a trigger
// that changes what it watches does not run forever.
func (r *Runner) onEvent(t *tenant, app, trigger, graph, node, queue string, accept func(live.Event) bool, msg jetstream.Msg) {
	md, err := msg.Metadata()
	var e live.Event
	if err == nil {
		err = json.Unmarshal(msg.Data(), &e)
	}
	if err != nil {
		log.Error(r.ctx, err, "jobs: the trigger %s of %s cannot read an event; skipped", trigger, app)
		_ = msg.Term()
		return
	}
	if e.Actor == "app:"+app || (accept != nil && !accept(e)) {
		_ = msg.Ack()
		return
	}
	e.Stream, e.Seq = md.Stream, md.Sequence.Stream
	inputs, _ := json.Marshal(map[string]any{"event": e})
	id := fmt.Sprintf("%s-%s-%d", app, trigger, e.Seq)
	req := Request{ID: id, Graph: graph, Event: node, Trigger: trigger, App: app, Inputs: inputs, Queue: queue,
		As: features.AppPrincipal(t.id, app).Ref(), Request: e.RequestID}
	res := r.send(t.id, "job.enqueue", id, "enqueue-"+id, e.RequestID, req)
	if res.Common.Code >= 300 {
		log.With("appId", app, "trigger", trigger).Error(r.ctx, log.Errorf("%s %+v", res.Common.Message, res.Problems), "jobs: cannot enqueue the trigger's job; trying again")
		_ = msg.NakWithDelay(time.Second)
		return
	}
	_ = msg.Ack()
}

// schedulePattern is On schedule's config as a message schedule.
func schedulePattern(config json.RawMessage) (pattern, zone string, err error) {
	var c livenodes.OnScheduleConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &c); err != nil {
			return "", "", fmt.Errorf("On schedule's config cannot be read: %v", err)
		}
	}
	switch {
	case c.Every != "":
		d, err := time.ParseDuration(c.Every)
		if err != nil || d < time.Second {
			return "", "", fmt.Errorf("%q is not an interval of at least 1s", c.Every)
		}
		return "@every " + d.String(), "", nil
	case c.At != "":
		at, err := time.Parse("15:04", c.At)
		if err != nil {
			return "", "", fmt.Errorf("%q is not a time of day (HH:MM)", c.At)
		}
		if c.TimeZone != "" {
			if _, err := time.LoadLocation(c.TimeZone); err != nil {
				return "", "", fmt.Errorf("%q is not a time zone", c.TimeZone)
			}
		}
		return fmt.Sprintf("0 %d %d * * *", at.Minute(), at.Hour()), c.TimeZone, nil
	}
	return "", "", fmt.Errorf("On schedule needs every or at")
}

// schedule makes sure an On schedule trigger's message schedule is what
// the install says: the server puts a tick on the trigger's queue each
// time it is due, wherever the runners are. Ticks of one trigger share a
// key, so they never overlap.
func (r *Runner) schedule(t *tenant, in apps.Install, d desired) error {
	if err := r.serveQueue(t, d.trigger.Queue); err != nil {
		return err
	}
	body, _ := json.Marshal(work{Tick: &tick{App: in.App, Trigger: d.trigger.ID, Queue: queueOf(d.trigger.Queue)}})
	m := nats.NewMsg(ScheduleSubject(t.id, in.App, d.trigger.ID))
	m.Data = body
	m.Header.Set("Nats-Schedule", d.schedule)
	if d.zone != "" {
		m.Header.Set("Nats-Schedule-Time-Zone", d.zone)
	}
	m.Header.Set("Nats-Schedule-Target", RunSubject(t.id, d.trigger.Queue, "trigger:"+in.App+"/"+d.trigger.ID))
	m.Header.Set("Nats-Rollup", "sub")
	// Every runner sends it; one message id per schedule drops the rest
	sum := sha256.Sum256([]byte(d.schedule + "|" + d.zone + "|" + string(body)))
	m.Header.Set(jetstream.MsgIDHeader, "schedule-"+in.App+"-"+d.trigger.ID+"-"+hex.EncodeToString(sum[:8]))
	if last, err := t.stream.GetLastMsgForSubject(r.ctx, m.Subject); err == nil &&
		last.Header.Get("Nats-Schedule") == d.schedule && last.Header.Get("Nats-Schedule-Time-Zone") == d.zone && string(last.Data) == string(body) {
		return nil // as it is already: sending it again would move its next tick
	}
	_, err := r.d.Projections.JetStream().PublishMsg(r.ctx, m)
	return err
}

// tickJob is the job of an On schedule tick, nil when the trigger no
// longer runs (the app uninstalled, the trigger removed).
func (r *Runner) tickJob(t *tenant, tk tick, msg jetstream.Msg) *Request {
	in, ok := installed(r.ctx, r.d.Projections, t.id, tk.App)
	if !ok {
		return nil
	}
	_, g, ok := in.Version.Trigger(tk.Trigger)
	if !ok {
		return nil
	}
	md, _ := msg.Metadata()
	due := md.Timestamp.UTC()
	inputs, _ := json.Marshal(map[string]any{"event": map[string]any{"time": due.Format(time.RFC3339)}, "time": due.Format(time.RFC3339)})
	return &Request{Graph: g.ID, Event: livenodes.OnSchedule, Trigger: tk.Trigger, App: tk.App, Queue: tk.Queue, Inputs: inputs,
		As: features.AppPrincipal(t.id, tk.App).Ref(), Key: "trigger:" + tk.App + "/" + tk.Trigger}
}

// outboxName is an area's outbox follower.
func outboxName(area live.Area) string { return "jobs.outbox." + string(area) }

// Outbox makes sure the principal's tenant's area has its outbox followed,
// before a command there enqueues a job (the Enqueue job node in a command
// graph): the follower is made, on the server, before the commit, so it
// sees the commit's jobs whichever process follows it.
func (r *Runner) Outbox(args features.ServiceArgs, area live.Area) error {
	t := r.tenantOf(args.Principal.TenantID)
	if t == nil {
		// Not served by this process: make the follower so a runner that
		// serves the tenant finds it
		return r.d.Projections.FollowNew(r.ctx, args.Principal.TenantID, area, outboxName(area), []string{live.StreamSubjects(args.Principal.TenantID, area)}, func(e live.Event) error {
			return r.enqueueOutbox(args.Principal.TenantID, e)
		})
	}
	return r.followOutbox(t, area)
}

func (r *Runner) followOutbox(t *tenant, area live.Area) error {
	t.mu.Lock()
	followed := t.outboxes[area]
	t.mu.Unlock()
	if followed {
		return nil
	}
	err := r.d.Projections.FollowNew(r.ctx, t.id, area, outboxName(area), []string{live.StreamSubjects(t.id, area)}, func(e live.Event) error {
		return r.enqueueOutbox(t.id, e)
	})
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.outboxes[area] = true
	t.mu.Unlock()
	return nil
}

// enqueueOutbox enqueues the jobs a commit carried, as the principal who
// sent the command; a job refused is logged and dropped.
func (r *Runner) enqueueOutbox(tenant uint64, e live.Event) error {
	for _, j := range e.Jobs {
		req := Request{ID: j.ID, Graph: j.Graph, Inputs: j.Inputs, Queue: j.Queue, Key: j.Key, RunAt: j.RunAt, App: j.App, As: j.As, Request: e.RequestID}
		res := r.send(tenant, "job.enqueue", j.ID, "enqueue-"+j.ID, e.RequestID, req)
		switch {
		case res.Common.Code >= 500:
			return fmt.Errorf("cannot enqueue job %s: %s", j.ID, res.Common.Message)
		case res.Common.Code >= 300:
			log.With("jobId", j.ID).Error(r.ctx, log.Code("job_refused").Errorf("%s %+v", res.Common.Message, res.Problems), "jobs: a job a command enqueued was refused")
		}
	}
	return nil
}

// Enqueue enqueues a job to run as the principal (the Enqueue job node
// outside command graphs): its graph must be published (or its app's
// installed version's). In a sandboxed run nothing is enqueued.
func (r *Runner) Enqueue(args features.ServiceArgs, req Request) (Job, error) {
	if req.ID == "" {
		req.ID = "job-" + fmt.Sprint(features.NewTSID())
	}
	if req.As.Kind == "" {
		req.As = args.Principal.Ref()
	}
	if _, _, err := r.graphFor(features.ServiceArgs{Ctx: args.Context(), Principal: args.Principal}, Job{Request: req}); err != nil {
		return Job{}, err
	}
	if bus.DryOf(args.Context()) != nil {
		return Job{Request: req, Status: StatusQueued}, nil
	}
	raw, _ := json.Marshal(req)
	res := r.d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "enqueue-" + req.ID, Type: "job.enqueue", Target: req.ID, RequestID: req.Request, Args: raw})
	if res.Common.Code >= 300 {
		msg := res.Common.Message
		if len(res.Problems) > 0 {
			msg = res.Problems[0].Message
		}
		return Job{}, log.Code("job_refused").Errorf("%s", msg)
	}
	j, _ := r.job(args.Principal.TenantID, req.ID, res.Seq)
	return j, nil
}

// Cancel cancels a job that has not started, as the principal.
func (r *Runner) Cancel(args features.ServiceArgs, id string) error {
	if bus.DryOf(args.Context()) != nil {
		return nil
	}
	res := r.d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "cancel-" + id + "-" + fmt.Sprint(features.NewTSID()), Type: "job.cancel", Target: id, Args: json.RawMessage(`{}`)})
	if res.Common.Code >= 300 {
		msg := res.Common.Message
		if len(res.Problems) > 0 {
			msg = res.Problems[0].Message
		}
		return log.Code("job_not_canceled").Errorf("%s", msg)
	}
	return nil
}
