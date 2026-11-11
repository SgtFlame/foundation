package connections_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	connectorsnodes "github.com/SgtFlame/foundation/backend/features/connectors/nodes"
	"github.com/SgtFlame/foundation/backend/features/connectors/scripted"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	jobsnodes "github.com/SgtFlame/foundation/backend/features/jobs/nodes"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

func init() {
	// Leases and polls on a test's time scale
	connectors.LeaseTTL = 1500 * time.Millisecond
}

// memSecrets is a SecretStore in memory: the placeholder names the value
// by a counter, never holds it.
type memSecrets struct {
	mu     sync.Mutex
	values map[string]string
	n      int
}

func (s *memSecrets) Put(_ context.Context, ref config.SecretRef, value string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	s.values[ref.Name()] = value
	return map[string]any{"$mem": s.n}, nil
}

func (s *memSecrets) Get(_ context.Context, ref config.SecretRef, _ map[string]any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[ref.Name()]
	if !ok {
		return "", fmt.Errorf("no secret %s", ref.Name())
	}
	return v, nil
}

func (s *memSecrets) Delete(_ context.Context, ref config.SecretRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, ref.Name())
	return nil
}

func (s *memSecrets) Hints() bool { return false }

func (s *memSecrets) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.values)
}

// hits records test.hit runs.
type hits struct {
	mu   sync.Mutex
	list []any
}

func (h *hits) add(v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.list = append(h.list, v)
}

func (h *hits) all() []any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.list)
}

type fixture struct {
	t        *testing.T
	srv      *natsd.Server
	reg      *extensions.Registry
	p        *project.Projections
	bus      *bus.Bus
	subs     *subscribe.Subscriptions
	deps     connections.Deps
	jobs     jobs.Deps
	secrets  *memSecrets
	hits     *hits
	scripted scripted.Deps
	n        atomic.Int64
}

type jobSettings struct{}

func (jobSettings) Resolved(features.ServiceArgs, string) (json.RawMessage, error) {
	return json.Marshal(jobs.Settings{MaxConcurrent: 4, MaxAttempts: 3, BackoffSeconds: 1, MaxBackoffSeconds: 1, DoneHours: 24})
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	reg.Declare(connectors.Contracts, connectors.Types, livenodes.TriggerEvents)
	cognitionnodes.Contribute(reg)
	uinodes.Contribute(reg)
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	var (
		nodeDeps   livenodes.Deps
		draftDeps  drafts.Deps
		entityDeps entities.Deps
		dataDeps   data.Deps
		prefabDeps prefabs.Deps
		appDeps    manifests.Deps
		installDep installs.Deps
		jobDeps    jobs.Deps
		jobNodes   jobsnodes.Deps
		cxDeps     connectors.Deps
		connDeps   connections.Deps
		scrDeps    scripted.Deps
	)
	livenodes.Contribute(reg, func() livenodes.Deps { return nodeDeps })
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return dataDeps })
	recordsnodes.Contribute(reg, func() data.Deps { return dataDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	manifests.Contribute(reg, func() manifests.Deps { return appDeps })
	installs.Contribute(reg, func() installs.Deps { return installDep })
	jobs.Contribute(reg, func() jobs.Deps { return jobDeps })
	jobsnodes.Contribute(reg, func() jobsnodes.Deps { return jobNodes })
	connectorstest.Contribute(reg)
	connectors.Contribute(reg, func() connectors.Deps { return cxDeps })
	connectorsnodes.Contribute(reg, func() connectors.Deps { return cxDeps })
	connections.Contribute(reg, func() connections.Deps { return connDeps })
	connections.ContributeTrigger(reg, func() connections.Deps { return connDeps })
	scripted.Contribute(reg, func() scripted.Deps { return scrDeps })
	h := &hits{}
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.hit", Source: "test"}, Title: "Hit", Category: "Test", Effects: true,
		Description: "Records its value, for tests", Example: "Hit",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), {ID: "value", Name: "Value", Type: cognition.Any, Optional: true, Description: "Recorded"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			h.add(in["value"])
			return nil, cognition.Then, nil
		},
	})
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(cognition.Models(), records.Models(), ui.Models(), apps.Models())...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	eng := engine.New(gdb, reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	f := &fixture{t: t, srv: srv, reg: reg, p: p, bus: bus.New(p), subs: subscribe.New(p, reg), secrets: &memSecrets{values: map[string]string{}}, hits: h}
	nodeDeps = livenodes.Deps{Engine: eng, Bus: f.bus, Subscriptions: f.subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	dataDeps = data.Deps{Projections: p, Bus: f.bus}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	f.bus.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	f.subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	jobDeps = jobs.Deps{Projections: p, Bus: f.bus, Engine: eng, Config: jobSettings{}}
	f.jobs = jobDeps
	registry := connectors.NewRegistry(reg, srv.JS())
	registry.SetCheckEvery(50 * time.Millisecond)
	t.Cleanup(registry.Close)
	conns := connections.Connections{Deps: func() connections.Deps { return connDeps }}
	dispatcher := &connectors.Dispatcher{NC: srv.Conn(), Registry: registry, Connections: conns, Failed: conns.Failed, Timeout: 3 * time.Second}
	cxDeps = connectors.Deps{Registry: registry, Dispatcher: dispatcher}
	connDeps = connections.Deps{Projections: p, Bus: f.bus, Registry: registry, Secrets: f.secrets, Dispatcher: dispatcher}
	f.deps = connDeps
	scrDeps = scripted.Deps{Store: p, Engine: eng, JS: srv.JS(), Reg: reg}
	f.scripted = scrDeps
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := connections.Serve(ctx, srv.Conn(), func() connections.Deps { return connDeps }); err != nil {
		t.Fatal(err)
	}
	runner := jobs.New(jobDeps, jobs.Options{Name: "nodes"})
	t.Cleanup(runner.Close)
	jobNodes = jobsnodes.Deps{Runner: runner, Store: p}
	return f
}

// keep runs the sources' keeper, as every server process does.
func (f *fixture) keep() {
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	go connections.Start(ctx, func() connections.Deps { return f.deps }, func() ([]uint64, error) { return []uint64{features.LocalTenantID}, nil })
}

// runJobs starts a process running tenant 1's jobs.
func (f *fixture) runJobs() {
	r := jobs.New(f.jobs, jobs.Options{Name: "runner", AckWait: time.Second, Discover: 200 * time.Millisecond})
	f.t.Cleanup(r.Close)
	if err := r.Serve(features.LocalTenantID); err != nil {
		f.t.Fatal(err)
	}
}

func user(id uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: features.LocalTenantID}}
}

func args() features.ServiceArgs { return user(1) }

func (f *fixture) doAs(as features.ServiceArgs, typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: as, ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.doAs(args(), typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

func (f *fixture) wait(projector, key string, seq uint64) live.Projected {
	f.t.Helper()
	v, err := f.p.WaitFor(context.Background(), features.LocalTenantID, projector, key, seq, 3*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s %s did not reach %d: %v", projector, key, seq, err)
	}
	return v
}

// connection reads a connection once the projection has seq.
func (f *fixture) connection(id string, seq uint64) connections.Connection {
	f.t.Helper()
	v := f.wait(connections.Projector, connections.TargetType+"."+id, seq)
	var c connections.Connection
	_ = json.Unmarshal(v.Value, &c)
	return c
}

// create creates a connection as user 1 and waits for it.
func (f *fixture) create(a map[string]any) connections.Connection {
	f.t.Helper()
	res := f.ok("connection.create", "", a)
	return f.connection(res.Target, res.Seq)
}

// serve starts an adapter on its own connection, as another process would.
func (f *fixture) serve(o adapter.Options) (stop func()) {
	f.t.Helper()
	nc, err := f.srv.Connect(o.Name)
	if err != nil {
		f.t.Fatal(err)
	}
	o.AnnounceEvery, o.TTL = 100*time.Millisecond, 400*time.Millisecond
	o.LeaseEvery = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- adapter.Serve(ctx, nc, o) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			if err := <-done; err != nil {
				f.t.Errorf("serve %s: %v", o.Name, err)
			}
			nc.Close()
		})
	}
	f.t.Cleanup(stop)
	return stop
}

func (f *fixture) online(typ string) {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		ti, ok, _ := f.deps.Registry.Type(context.Background(), 1, typ)
		if ok && ti.Online {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("%s is not online", typ)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (f *fixture) call(as features.ServiceArgs, conn, op string, a string) (map[string]any, *connectors.Error) {
	raw, e := f.deps.Dispatcher.Call(as, connectors.Call{Contract: connectorstest.ContractRef, Operation: op, Connection: conn, Args: json.RawMessage(a)})
	if e != nil {
		return nil, e
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

// eventually waits until ok, failing with what after within.
func eventually(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen in %s", what, within)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// source reads a connection's cx_sources entry.
func (f *fixture) source(id string) (connectors.Source, bool) {
	kv, err := connectors.SourcesKV(context.Background(), f.srv.JS())
	if err != nil {
		f.t.Fatal(err)
	}
	e, err := kv.Get(context.Background(), connectors.SourceKey(1, id))
	if err != nil {
		return connectors.Source{}, false
	}
	var s connectors.Source
	_ = json.Unmarshal(e.Value(), &s)
	return s, true
}

func node(id, typ string, config map[string]any) map[string]any {
	n := map[string]any{"id": id, "type": typ, "position": map[string]any{"x": 0, "y": 0}}
	if config != nil {
		n["config"] = config
	}
	return n
}

func edge(fromNode, fromPin, toNode, toPin string) map[string]any {
	return map[string]any{"from": map[string]any{"node": fromNode, "pin": fromPin}, "to": map[string]any{"node": toNode, "pin": toPin}}
}

// installWatcher makes, publishes and installs the Watcher app: a trigger
// On connector event test.echo@1 finished (of connection when given) that
// hits with the event's jobId.
func (f *fixture) installWatcher(connection string) {
	f.t.Helper()
	own := "app:watcher"
	cfg := map[string]any{"contract": connectorstest.ContractRef, "event": "finished"}
	if connection != "" {
		cfg["connection"] = connection
	}
	g := map[string]any{"name": "On finished", "kind": livenodes.KindTrigger, "owner": own,
		"nodes": []any{node("event", connectorsnodes.On, cfg), node("hit", "test.hit", nil)},
		"edges": []any{edge("event", "then", "hit", "then"), edge("event", "jobId", "hit", "value")},
	}
	res := f.ok("bp.import", "watcher-finished", map[string]any{"graph": g})
	var d drafts.Draft
	_ = json.Unmarshal(f.wait(drafts.DraftProjector, "graph.watcher-finished", res.Seq).Value, &d)
	res = f.ok("bp.publish", "watcher-finished", map[string]any{"rev": d.Rev})
	f.wait(drafts.PublishedProjector, "graph.watcher-finished", res.Seq)
	f.ok("app.create", "", map[string]any{"name": "Watcher"})
	f.ok("app.addTrigger", "watcher", map[string]any{"id": "finished", "graph": "watcher-finished"})
	var md manifests.Draft
	seq := f.ok("app.setInfo", "watcher", map[string]any{"description": "Watches"}).Seq
	_ = json.Unmarshal(f.wait(manifests.DraftProjector, "app.watcher", seq).Value, &md)
	res = f.ok("app.publish", "watcher", map[string]any{"rev": md.Rev})
	f.wait(manifests.VersionProjector, "appVersion."+apps.VersionID("watcher", 1), res.Seq)
	res = f.ok("app.install", "watcher", map[string]any{})
	f.wait(installs.Projector, "appInstall.watcher", res.Seq)
}

// syncBuffer is a buffer safe for concurrent writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var _ = strings.Contains
