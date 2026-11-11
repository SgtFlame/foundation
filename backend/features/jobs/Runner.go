package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Deps are what the runner, commands and query need, made after the
// registry.
type Deps struct {
	Projections *project.Projections
	Bus         *bus.Bus
	Engine      *engine.Engine
	Config      config.Reader // the jobs section; nil uses Defaults
}

// Options tune a runner; zero values take the defaults.
type Options struct {
	// Name is this runner's name in its jobs' runs: host-pid when empty.
	Name string
	// AckWait is how long a job a runner took stays with it after the
	// runner stops answering, before another runner takes it: 30s.
	AckWait time.Duration
	// Discover is how often a runner looks for queues, areas with jobs
	// and tenants it has not seen: 5s.
	Discover time.Duration
}

// Runner runs a process's share of the jobs of the tenants it serves.
// Every process may have one; runners of one tenant in several processes
// share its queues, so each job runs once.
type Runner struct {
	d       Deps
	o       Options
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	tenants map[uint64]*tenant
	wg      sync.WaitGroup
	closing atomic.Bool
}

// tenant is one tenant a runner serves.
type tenant struct {
	id       uint64
	stream   jetstream.Stream
	mu       sync.Mutex
	queues   map[string]bool                      // queues with a worker here
	triggers map[string]jetstream.ConsumeContext // trigger consumers consumed here, by area/name
	outboxes map[live.Area]bool                   // areas whose outbox is followed here
	wake     chan struct{}                        // a worker freed a slot
	// pending are apps with a trigger that could not start yet (its app's
	// roles not synced yet, by whichever process syncs them), reconciled
	// again on each discovery; warned is what was last logged per trigger
	pending map[string]bool
	warned  map[string]string
}

// New makes a runner; Serve starts it on a tenant.
func New(d Deps, o Options) *Runner {
	if o.Name == "" {
		host, _ := os.Hostname()
		o.Name = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	if o.AckWait <= 0 {
		o.AckWait = 30 * time.Second
	}
	if o.Discover <= 0 {
		o.Discover = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{d: d, o: o, ctx: ctx, cancel: cancel, tenants: map[uint64]*tenant{}}
}

// Name is the runner's name.
func (r *Runner) Name() string { return r.o.Name }

// Close stops the runner as a dying process would: it takes no more jobs,
// and the jobs it is running are left unacknowledged, so another runner
// runs them again after the ack wait. Their graphs' contexts end.
func (r *Runner) Close() {
	r.closing.Store(true)
	r.cancel()
	r.mu.Lock()
	for _, t := range r.tenants {
		t.mu.Lock()
		for _, c := range t.triggers {
			c.Stop()
		}
		t.mu.Unlock()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// Start serves every tenant tenants lists, now and as it lists more.
func (r *Runner) Start(tenants func() ([]uint64, error)) {
	serveAll := func() {
		ids, err := tenants()
		if err != nil {
			log.Error(r.ctx, err, "jobs: cannot list the tenants to serve")
			return
		}
		for _, id := range ids {
			if err := r.Serve(id); err != nil {
				log.Error(r.ctx, err, "jobs: cannot serve tenant %d", id)
			}
		}
	}
	serveAll()
	r.loop(r.o.Discover*12, serveAll)
}

// loop runs fn every period until the runner closes.
func (r *Runner) loop(period time.Duration, fn func()) {
	r.wg.Go(func() {
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-t.C:
				fn()
			}
		}
	})
}

// Serve starts serving a tenant's jobs: its dispatch follower, its queues,
// the outboxes of its areas and its installed apps' triggers. Serving a
// tenant served already does nothing.
func (r *Runner) Serve(id uint64) error {
	if r.closing.Load() {
		return nil
	}
	r.mu.Lock()
	if r.tenants[id] != nil {
		r.mu.Unlock()
		return nil
	}
	t := &tenant{id: id, queues: map[string]bool{}, triggers: map[string]jetstream.ConsumeContext{}, outboxes: map[live.Area]bool{}, wake: make(chan struct{}, 1),
		pending: map[string]bool{}, warned: map[string]string{}}
	r.tenants[id] = t
	r.mu.Unlock()
	fail := func(err error) error {
		r.mu.Lock()
		delete(r.tenants, id)
		r.mu.Unlock()
		return err
	}
	stream, err := r.stream(id)
	if err != nil {
		return fail(err)
	}
	t.stream = stream
	subject := fmt.Sprintf("evt.%d.%s.%s.*.*", id, Area, TargetType)
	if err := r.d.Projections.Follow(r.ctx, id, Area, "jobs.dispatch", []string{subject}, func(e live.Event) error { return r.dispatch(t, e) }); err != nil {
		return fail(err)
	}
	r.discover(t)
	r.loop(r.o.Discover, func() { r.discover(t) })
	r.wg.Go(func() { r.watchInstalls(t) })
	return nil
}

// tenantOf is the served tenant, nil when it is not served here.
func (r *Runner) tenantOf(id uint64) *tenant {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tenants[id]
}

// stream makes sure the tenant's work queue exists: each message is taken
// by one runner and removed when it acknowledges it; delayed jobs and
// schedules are its message schedules.
func (r *Runner) stream(tenant uint64) (jetstream.Stream, error) {
	s, err := r.d.Projections.JetStream().CreateOrUpdateStream(r.ctx, jetstream.StreamConfig{
		Name: StreamName(tenant), Subjects: []string{Subjects(tenant)}, Storage: jetstream.FileStorage,
		Retention: jetstream.WorkQueuePolicy, Duplicates: project.Duplicates, AllowDirect: true,
		AllowMsgSchedules: true, AllowRollup: true,
	})
	if err != nil {
		return nil, log.Wrapf(err, "cannot make the job queue %s", StreamName(tenant))
	}
	return s, nil
}

// discover attaches this runner to what other runners made: queues with
// a consumer, and areas whose outbox is followed; and tries again the
// triggers that could not start.
func (r *Runner) discover(t *tenant) {
	ctx := r.ctx
	t.mu.Lock()
	pending := slices.Collect(maps.Keys(t.pending))
	t.mu.Unlock()
	for _, app := range pending {
		in, ok := installs.Get(ctx, r.d.Projections, t.id, app)
		if !ok {
			continue
		}
		if err := r.reconcile(t, in); err != nil {
			log.With("appId", app).Error(ctx, err, "jobs: cannot keep the triggers of app %s", app)
		}
	}
	names := t.stream.ConsumerNames(ctx)
	for name := range names.Name() {
		if q, ok := strings.CutPrefix(name, "run_"); ok {
			r.serveQueue(t, q)
		}
	}
	streams := r.d.Projections.JetStream().StreamNames(ctx, jetstream.WithStreamListSubject(fmt.Sprintf("evt.%d.>", t.id)))
	prefix := fmt.Sprintf("EVT_%d_", t.id)
	for name := range streams.Name() {
		area := live.Area(strings.TrimPrefix(name, prefix))
		if area == Area || !strings.HasPrefix(name, prefix) {
			continue
		}
		t.mu.Lock()
		followed := t.outboxes[area]
		t.mu.Unlock()
		if !followed && r.d.Projections.Following(ctx, t.id, area, outboxName(area)) {
			if err := r.followOutbox(t, area); err != nil {
				log.Error(ctx, err, "jobs: cannot follow the outbox of %s", name)
			}
		}
	}
}

// settings are the tenant's jobs section.
func (r *Runner) settings(tenant uint64) Settings {
	if r.d.Config == nil {
		return Defaults
	}
	s, err := config.Read[Settings](r.d.Config, features.ServiceArgs{Ctx: r.ctx, Principal: features.ServicePrincipal(tenant)}, SettingsID)
	if err != nil {
		return Defaults
	}
	return settingsOf(s)
}

// send sends a job command as the tenant's service principal.
func (r *Runner) send(tenant uint64, typ, target, id, request string, args any) bus.Result {
	raw, _ := json.Marshal(args)
	if id == "" {
		id = typ + "-" + target + "-" + strconv.FormatUint(features.NewTSID(), 36)
	}
	return r.d.Bus.Execute(bus.Request{ServiceArgs: features.ServiceArgs{Ctx: r.ctx, Principal: features.ServicePrincipal(tenant)},
		ID: id, Type: typ, Target: target, RequestID: request, Args: raw})
}

// job reads a job's state, at least as of seq.
func (r *Runner) job(tenant uint64, id string, seq uint64) (Job, bool) {
	v, err := r.d.Projections.WaitFor(r.ctx, tenant, Projector, TargetType+"."+id, seq, 2*time.Second)
	var j Job
	if err != nil || v.Value == nil || json.Unmarshal(v.Value, &j) != nil {
		return Job{}, false
	}
	return j, true
}

// dispatch puts a queued job on its queue, as its delay's schedule when
// it is not due yet, and takes a canceled one's schedule off. Every
// runner's dispatch follower shares one consumer, so each event is
// dispatched once; a dispatch sent twice has one message id.
func (r *Runner) dispatch(t *tenant, e live.Event) error {
	switch e.Type {
	case EventQueued, EventRetried, EventCanceled:
	default:
		return nil
	}
	j, ok := r.job(t.id, e.Target.ID, e.Seq)
	if !ok {
		return fmt.Errorf("the job %s cannot be read", e.Target.ID)
	}
	js := r.d.Projections.JetStream()
	if e.Type == EventCanceled {
		err := t.stream.Purge(r.ctx, jetstream.WithPurgeSubject(AtSubject(t.id, j.ID)))
		if err != nil && !errors.Is(err, jetstream.ErrMsgNotFound) {
			return err
		}
		return nil
	}
	if j.Status != StatusQueued {
		return nil // started or canceled since
	}
	if err := r.serveQueue(t, j.Queue); err != nil {
		return err
	}
	body, _ := json.Marshal(work{ID: j.ID})
	m := nats.NewMsg(RunSubject(t.id, j.Queue, j.Key))
	m.Data = body
	m.Header.Set(jetstream.MsgIDHeader, fmt.Sprintf("%s#%d", j.ID, j.Dispatch))
	if time.Until(j.RunAt) > 500*time.Millisecond {
		// Delayed: the server puts it on its queue when it is due
		m.Subject = AtSubject(t.id, j.ID)
		m.Header.Set(jetstream.MsgIDHeader, fmt.Sprintf("%s#%d-at", j.ID, j.Dispatch))
		// Schedules are by the second: never before the job is due
		at := j.RunAt.UTC()
		if at.Truncate(time.Second) != at {
			at = at.Truncate(time.Second).Add(time.Second)
		}
		m.Header.Set("Nats-Schedule", "@at "+at.Format(time.RFC3339))
		m.Header.Set("Nats-Schedule-Target", RunSubject(t.id, j.Queue, j.Key))
		m.Header.Set("Nats-Rollup", "sub")
	}
	_, err := js.PublishMsg(r.ctx, m)
	return err
}

// serveQueue makes sure the queue's consumer exists and a worker of this
// runner pulls from it.
func (r *Runner) serveQueue(t *tenant, queue string) error {
	queue = queueOf(queue)
	t.mu.Lock()
	served := t.queues[queue]
	t.mu.Unlock()
	if served {
		return nil
	}
	cons, err := t.stream.CreateOrUpdateConsumer(r.ctx, jetstream.ConsumerConfig{
		Durable: ConsumerName(queue), FilterSubjects: []string{fmt.Sprintf("jobs.%d.run.%s.>", t.id, queue)},
		AckPolicy: jetstream.AckExplicitPolicy, AckWait: r.o.AckWait, MaxDeliver: -1, MaxAckPending: 10000,
	})
	if err != nil {
		return log.Wrapf(err, "cannot make the consumer of queue %s", queue)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.queues[queue] || r.closing.Load() {
		return nil
	}
	t.queues[queue] = true
	r.wg.Go(func() { r.work(t, queue, cons) })
	return nil
}

// work pulls the queue's jobs one at a time while it runs fewer than the
// queue's concurrency, and runs each.
func (r *Runner) work(t *tenant, queue string, cons jetstream.Consumer) {
	var running atomic.Int32
	var runs sync.WaitGroup
	defer runs.Wait()
	for r.ctx.Err() == nil {
		if int(running.Load()) >= r.settings(t.id).concurrency(queue) {
			select {
			case <-t.wake:
			case <-time.After(250 * time.Millisecond):
			case <-r.ctx.Done():
			}
			continue
		}
		ctx, cancel := context.WithTimeout(r.ctx, 2*time.Second)
		msg, err := cons.Next(jetstream.FetchContext(ctx))
		cancel()
		if err != nil {
			if errors.Is(err, jetstream.ErrConsumerNotFound) || errors.Is(err, jetstream.ErrConsumerDeleted) {
				t.mu.Lock()
				delete(t.queues, queue)
				t.mu.Unlock()
				return
			}
			continue
		}
		running.Add(1)
		runs.Go(func() {
			defer func() {
				running.Add(-1)
				select {
				case t.wake <- struct{}{}:
				default:
				}
			}()
			r.handle(t, msg)
		})
	}
}

// handle runs one work message's job, if it may run now: acknowledged when
// its run ends for good, sent back with a delay to be retried or to wait
// for its key, and left alone when the runner closes mid-run.
func (r *Runner) handle(t *tenant, msg jetstream.Msg) {
	md, err := msg.Metadata()
	var w work
	if err == nil {
		err = json.Unmarshal(msg.Data(), &w)
	}
	if err != nil {
		log.Error(r.ctx, err, "jobs: a work message on %s cannot be read; dropped", msg.Subject())
		_ = msg.Term()
		return
	}
	id := w.ID
	var create *Request
	if w.Tick != nil {
		// An On schedule tick: its job is made from the tick, once
		id = fmt.Sprintf("%s-%s-%d", w.Tick.App, w.Tick.Trigger, md.Sequence.Stream)
		if create = r.tickJob(t, *w.Tick, msg); create == nil {
			_ = msg.Ack()
			return
		}
	}
	// Jobs with one key run in order: this one only when no earlier one is
	// left on its subject (running or waiting)
	if !strings.HasSuffix(msg.Subject(), "._") {
		first, err := t.stream.GetMsg(r.ctx, 1, jetstream.WithGetMsgSubject(msg.Subject()))
		if err == nil && first.Sequence < md.Sequence.Stream {
			_ = msg.NakWithDelay(200 * time.Millisecond)
			return
		}
	}
	if j, ok := r.job(t.id, id, 0); ok {
		switch {
		case j.Finished():
			_ = msg.Ack()
			return
		case time.Until(j.RetryAt) > 0:
			_ = msg.NakWithDelay(time.Until(j.RetryAt)) // handed back early (a runner stopped)
			return
		case j.Status == StatusQueued && time.Until(j.RunAt) > 0:
			_ = msg.NakWithDelay(time.Until(j.RunAt)) // not due yet
			return
		case j.Status == StatusRunning && md.NumDelivered == 1:
			// Running elsewhere: this is a copy of its message
			_ = msg.Ack()
			return
		}
	}
	res := r.send(t.id, "job.start", id, "", "", startArgs{Runner: r.o.Name, Job: create})
	if res.Common.Code >= 300 {
		if len(res.Problems) > 0 && (res.Problems[0].Code == "job_finished" || res.Problems[0].Code == "job_not_found") {
			_ = msg.Ack()
			return
		}
		log.Error(r.ctx, log.Errorf("%s %+v", res.Common.Message, res.Problems), "jobs: cannot start job %s; trying again", id)
		_ = msg.NakWithDelay(time.Second)
		return
	}
	j, ok := r.job(t.id, id, res.Seq)
	if !ok || j.Status != StatusRunning {
		_ = msg.NakWithDelay(time.Second)
		return
	}
	// The job stays with this runner while it runs
	alive := make(chan struct{})
	r.wg.Go(func() {
		tick := time.NewTicker(max(r.o.AckWait/3, 100*time.Millisecond))
		defer tick.Stop()
		for {
			select {
			case <-alive:
				return
			case <-r.ctx.Done():
				return
			case <-tick.C:
				_ = msg.InProgress()
			}
		}
	})
	fin := r.run(t, j)
	close(alive)
	if r.closing.Load() {
		return // a stopped runner's job runs again elsewhere
	}
	for try := 0; ; try++ {
		res := r.send(t.id, "job.finish", id, fmt.Sprintf("finish-%s-%d-%d", id, j.Dispatch, j.Attempts), "", fin)
		if res.Common.Code < 300 {
			break
		}
		if try == 4 || r.ctx.Err() != nil {
			log.Error(r.ctx, log.Errorf("%s", res.Common.Message), "jobs: cannot record the end of job %s; it will run again", id)
			_ = msg.Nak()
			return
		}
		time.Sleep(time.Duration(try+1) * 200 * time.Millisecond)
	}
	if fin.Outcome == OutcomeRetry {
		_ = msg.NakWithDelay(max(time.Until(fin.RetryAt), 0))
		return
	}
	_ = msg.Ack()
}

// run runs a job's attempt and says how it ended.
func (r *Runner) run(t *tenant, j Job) finishArgs {
	fin := finishArgs{Attempt: j.Attempts, Outcome: OutcomeDone}
	dead := func(err error) finishArgs {
		fin.Outcome, fin.Error = OutcomeDead, err.Error()
		return fin
	}
	info := Info{ID: j.ID, Attempt: j.Attempts, Queue: j.Queue, App: j.App, Trigger: j.Trigger}
	args := features.ServiceArgs{Ctx: r.ctx, Principal: j.As.In(t.id)}
	args.Meta = args.Meta.WithFeature("jobs")
	g, pinned, err := r.graphFor(args, j)
	if err != nil {
		return dead(err)
	}
	fin.Version = g.Version
	event, inputs, err := r.inputs(args, j, g, &info)
	if err != nil {
		return dead(err)
	}
	args.Ctx = WithInfo(r.ctx, info)
	run := engine.RunArgs{ServiceArgs: args, GraphID: g.ID, Event: event, Inputs: inputs, Trigger: "job:" + j.ID, Trace: engine.TraceAll}
	if pinned {
		run.Graph = &g
	}
	res, err := r.d.Engine.Run(run)
	fin.RunID, fin.FailedNode = res.RunID, res.FailedNode
	if err == nil {
		return fin
	}
	fin.Error = err.Error()
	retry, after := Classify(err)
	s := r.settings(t.id)
	maxAttempts := s.MaxAttempts
	if j.MaxAttempts > 0 {
		maxAttempts = j.MaxAttempts
	}
	if !retry || j.Attempts >= maxAttempts {
		fin.Outcome = OutcomeDead
		if retry {
			fin.Error = fmt.Sprintf("%s (after %d attempts)", fin.Error, j.Attempts)
		}
		return fin
	}
	if after <= 0 {
		after = Backoff(j.Attempts, time.Duration(s.BackoffSeconds)*time.Second, time.Duration(s.MaxBackoffSeconds)*time.Second)
	}
	fin.Outcome, fin.RetryAt = OutcomeRetry, time.Now().Add(after).UTC()
	return fin
}

// graphFor is the graph a job runs: its app's installed version's copy
// (pinned) for an app's job or a trigger's, else the tenant's published
// graph.
func (r *Runner) graphFor(args features.ServiceArgs, j Job) (cognition.Graph, bool, error) {
	if j.App != "" {
		in, ok := installed(args.Context(), r.d.Projections, args.Principal.TenantID, j.App)
		if !ok {
			return cognition.Graph{}, false, fmt.Errorf("the app %s is not installed", j.App)
		}
		if j.Trigger != "" {
			_, g, ok := in.Version.Trigger(j.Trigger)
			if !ok {
				return g, false, fmt.Errorf("the app %s has no trigger %s with a graph any more", j.App, j.Trigger)
			}
			return g, true, nil
		}
		if g, ok := in.Version.Graphs[j.Graph]; ok {
			return g, true, nil
		}
	}
	g, ok := r.d.Engine.Graph(args, j.Graph)
	if !ok {
		return g, false, fmt.Errorf("there is no published graph %s", j.Graph)
	}
	if g.ID == "" {
		g.ID = j.Graph
	}
	return g, false, nil
}

// inputs are the graph's event and its outputs for the job: a trigger
// event's from its event (read as the job's principal), a tick's time, or
// the job's own inputs for On call.
func (r *Runner) inputs(args features.ServiceArgs, j Job, g cognition.Graph, info *Info) (string, cognition.Values, error) {
	in := cognition.Values{}
	if len(j.Inputs) > 0 {
		if err := json.Unmarshal(j.Inputs, &in); err != nil {
			return "", nil, fmt.Errorf("the job's inputs cannot be read: %v", err)
		}
	}
	event := j.Event
	if event == "" {
		if g.Kind == "app.trigger" {
			return "", nil, fmt.Errorf("the trigger graph %s runs only from its trigger", g.ID)
		}
		return cognition.EventCall, in, nil
	}
	if ev, ok := in["event"].(map[string]any); ok {
		info.Event = ev
	}
	te, ok := triggerEvent(r.d.Engine, event)
	if !ok {
		return event, in, nil // On schedule's tick, or an event with nothing to read
	}
	var config json.RawMessage
	for _, n := range g.Nodes {
		if n.Type == event {
			config = n.Config
		}
	}
	var e live.Event
	raw, _ := json.Marshal(in["event"])
	if err := json.Unmarshal(raw, &e); err != nil {
		return "", nil, fmt.Errorf("the trigger's event cannot be read: %v", err)
	}
	out, err := te.Inputs(args, config, e)
	return event, out, err
}
