package connections_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	connectorsnodes "github.com/SgtFlame/foundation/backend/features/connectors/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const secret = "hunter2-very-secret"

// sink collects a subscription's latest value.
type sink struct {
	mu  sync.Mutex
	v   any
	got chan struct{}
}

func (s *sink) Snapshot(data any, _ live.Position) {
	raw, _ := json.Marshal(data)
	s.mu.Lock()
	_ = json.Unmarshal(raw, &s.v)
	s.mu.Unlock()
	select {
	case s.got <- struct{}{}:
	default:
	}
}
func (s *sink) Change(d live.Delta, _ live.Position) {
	s.mu.Lock()
	s.v = live.Apply(s.v, d)
	s.mu.Unlock()
	select {
	case s.got <- struct{}{}:
	default:
	}
}
func (s *sink) Failed(resp features.ServiceResponse) { panic(resp.Message) }

func (s *sink) json() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.v)
	return string(raw)
}

func (f *fixture) query(as features.ServiceArgs, id string, params any) *sink {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	as.Ctx = ctx
	raw, _ := json.Marshal(params)
	snk := &sink{got: make(chan struct{}, 1)}
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: as, Query: id, Params: raw, Sink: snk}); fail != nil {
		f.t.Fatal(fail.Message)
	}
	select {
	case <-snk.got:
	case <-time.After(3 * time.Second):
		f.t.Fatalf("%s sent nothing", id)
	}
	return snk
}

// A connection's secret goes to the SecretStore and is in no response,
// event, op, query result, log line, tool result or call on the bus; the
// adapter's session opens with it all the same, through cx.resolve.
func TestSecretNeverShown(t *testing.T) {
	logs := &syncBuffer{}
	if err := logging.Setup(logs, "json", slog.LevelDebug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logging.Setup(os.Stderr, "text", slog.LevelInfo) })
	f := setup(t)
	// Every message on the bus but the replies to cx.resolve, which carry
	// the settings to the adapter
	var traffic syncBuffer
	var resolveReplies sync.Map
	tap, err := f.srv.Connect("tap")
	if err != nil {
		t.Fatal(err)
	}
	defer tap.Close()
	if _, err := tap.Subscribe(">", func(m *nats.Msg) {
		if strings.HasPrefix(m.Subject, "cx.resolve.") {
			resolveReplies.Store(m.Reply, true)
		}
		if _, ok := resolveReplies.Load(m.Subject); ok {
			return
		}
		fmt.Fprintf(&traffic, "%s %v %s\n", m.Subject, m.Header, m.Data)
	}); err != nil {
		t.Fatal(err)
	}
	_ = tap.Flush()
	f.serve(connectorstest.Options("s", nil))
	f.online("test.secure")

	res := f.ok("connection.create", "", map[string]any{"type": "test.secure", "name": "Support", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": secret}})
	c := f.connection(res.Target, res.Seq)
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), secret) || c.Secrets["password"] == "" || f.secrets.count() != 1 {
		t.Fatalf("the create: %s, the connection %+v, %d secrets stored", raw, c, f.secrets.count())
	}
	out, e := f.call(args(), c.ID, "echo", `{"text":"hello"}`)
	if e != nil || out["text"] != "hello" {
		t.Fatalf("echo through the connection: %v %v", out, e)
	}
	// An update with an empty secret keeps it; a new one replaces it
	res = f.ok("connection.update", c.ID, map[string]any{"settings": map[string]any{"user": "v"}, "secrets": map[string]any{"password": ""}})
	if c2 := f.connection(c.ID, res.Seq); c2.Secrets["password"] != c.Secrets["password"] || c2.Settings["user"] != "v" {
		t.Errorf("an empty secret did not keep it: %+v", c2)
	}
	res = f.ok("connection.update", c.ID, map[string]any{"secrets": map[string]any{"password": secret + "-2"}})
	if c2 := f.connection(c.ID, res.Seq); c2.Secrets["password"] == c.Secrets["password"] {
		t.Errorf("a new secret did not replace it: %+v", c2)
	}
	if out, e := f.call(args(), c.ID, "echo", `{"text":"again"}`); e != nil || out["text"] != "again" {
		t.Fatalf("echo after the update: %v %v", out, e)
	}
	list := f.query(args(), "connections.list", map[string]any{"contract": connectorstest.ContractRef})
	describe := f.query(args(), "connections.describe", map[string]any{"id": c.ID})
	if !strings.Contains(list.json(), `"password":{"set":true}`) || !strings.Contains(describe.json(), `"password":{"set":true}`) {
		t.Errorf("the queries do not say the secret is set:\n%s\n%s", list.json(), describe.json())
	}
	// The tool
	var tool tools.Tool
	for _, x := range tools.Tools.All(f.reg) {
		if x.ID == "connection.test" {
			tool = x
		}
	}
	result, err := tool.Run(tools.Call{ServiceArgs: args(), Args: json.RawMessage(`{"id":"` + c.ID + `"}`)})
	toolRaw, _ := json.Marshal(result)
	if err != nil || result.Content["ok"] != true {
		t.Errorf("connection.test: %s %v", toolRaw, err)
	}
	// Every event committed on the def stream
	stream, err := f.p.Stream(context.Background(), 1, live.AreaDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	var events strings.Builder
	info, _ := stream.Info(context.Background())
	for seq := uint64(1); seq <= info.State.LastSeq; seq++ {
		if m, err := stream.GetMsg(context.Background(), seq); err == nil {
			events.Write(m.Data)
		}
	}
	time.Sleep(100 * time.Millisecond)
	for what, text := range map[string]string{"an event or op": events.String(), "a query": list.json() + describe.json(), "the bus": traffic.String(),
		"a log line": logs.String(), "a tool result": string(toolRaw)} {
		if strings.Contains(text, secret) {
			t.Errorf("the secret is in %s:\n%s", what, text)
		}
	}
	if !strings.Contains(traffic.String(), "cx.call.1.test_secure."+c.ID+".echo") {
		t.Fatalf("the tap saw no call:\n%s", traffic.String())
	}
	// Deleting forgets every version of the secret
	f.keep()
	f.ok("connection.delete", c.ID, map[string]any{})
	eventually(t, "the secrets purged", 3*time.Second, func() bool { return f.secrets.count() == 0 })
}

func installPolicy(t *testing.T, f *fixture) *authz.Policy {
	appDB, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	authz.Contribute(f.reg)
	connections.ContributeAccess(f.reg)
	connectors.ContributeAccess(f.reg)
	pol := authz.NewPolicy(appDB, f.reg)
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })
	for _, g := range []authz.GrantArgs{
		{UserID: 1, RoleID: authz.RoleEditor, Resource: "tenant:1"},
		{UserID: 2, RoleID: authz.RoleEditor, Resource: "tenant:1"},
		{UserID: 3, RoleID: authz.RoleAdmin, Resource: "tenant:1"},
	} {
		if _, err := pol.Grant(context.Background(), nil, g); err != nil {
			t.Fatal(err)
		}
	}
	return pol
}

// A user's own connection is theirs: another member may not call it, nor
// a trigger of an app; a tenant connection needs connection.manage to
// make and use to call.
func TestUseRequired(t *testing.T) {
	f := setup(t)
	installPolicy(t, f)
	f.serve(connectorstest.Options("s", nil))
	f.online("test.secure")
	mine := f.create(map[string]any{"type": "test.secure", "name": "Mine", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": "p"}})
	if mine.Owner != "user:1" {
		t.Fatalf("owner %q", mine.Owner)
	}
	if _, e := f.call(user(1), mine.ID, "echo", `{"text":"x"}`); e != nil {
		t.Fatalf("the owner's call: %v", e)
	}
	if _, e := f.call(user(2), mine.ID, "echo", `{"text":"x"}`); e == nil || e.Code != connectors.CodeAuth {
		t.Errorf("another member called a user's connection: %v", e)
	}
	if _, e := f.call(user(3), mine.ID, "echo", `{"text":"x"}`); e == nil {
		t.Errorf("an administrator called a user's own connection")
	}
	// An Editor may not make a tenant connection; an Administrator may
	if res := f.doAs(user(2), "connection.create", "", map[string]any{"type": "test.secure", "name": "Shared", "owner": "tenant", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": "p"}}); res.Common.Code < 300 {
		t.Errorf("an Editor made a tenant connection")
	}
	n := f.secrets.count()
	res := f.doAs(user(3), "connection.create", "", map[string]any{"type": "test.secure", "name": "Shared", "owner": "tenant", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": "p"}})
	if res.Common.Code >= 300 || f.secrets.count() != n+1 {
		t.Fatalf("an Administrator's tenant connection: %+v %+v", res.Common, res.Problems)
	}
	shared := f.connection(res.Target, res.Seq)
	if _, e := f.call(user(2), shared.ID, "echo", `{"text":"x"}`); e != nil {
		t.Errorf("an Editor (execute) could not use a tenant connection: %v", e)
	}
	// A trigger filtered to a connection its app may not use is refused
	te, _ := livenodes.TriggerEventOf(f.reg, connectorsnodes.On)
	app := features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(1, "watcher")}
	cfg, _ := json.Marshal(map[string]any{"contract": connectorstest.ContractRef, "event": "finished", "connection": mine.ID})
	if _, _, err := te.Subjects(app, "watcher", cfg); err == nil || !strings.Contains(err.Error(), "may not use") {
		t.Errorf("a trigger on another's connection: %v", err)
	}
	cfg, _ = json.Marshal(map[string]any{"contract": connectorstest.ContractRef, "event": "finished"})
	if _, subjects, err := te.Subjects(app, "watcher", cfg); err != nil || subjects[0] != "cx.event.1.*.test_echo.finished" {
		t.Errorf("an unfiltered trigger: %v %v", subjects, err)
	}
	if te.Accept(app, cfg, live.Event{Target: live.Target{ID: mine.ID}}) {
		t.Errorf("an unfiltered trigger accepted an event of a connection its app may not use")
	}
}

// The test connector's event source reaches a trigger once per event: a
// holder that stores no cursor for its last events and is stopped makes
// its successor in another process emit them again, and JetStream drops
// them by their ids; the successor resumes from the cursor.
func TestEventSource(t *testing.T) {
	f := setup(t)
	f.keep()
	f.runJobs()
	f.installWatcher("")
	a := f.serve(connectorstest.Options("a", nil))
	f.online("test.secure")
	c := f.create(map[string]any{"type": "test.secure", "name": "Feed", "owner": "tenant", "settings": map[string]any{"user": "u", "events": 3, "loseCursorFrom": 3},
		"secrets": map[string]any{"password": "p"}, "options": map[string]any{"pollInterval": "1s"}})
	eventually(t, "the source listed", 3*time.Second, func() bool { _, ok := f.source(c.ID); return ok })
	eventually(t, "three hits", 5*time.Second, func() bool { return len(f.hits.all()) >= 3 })
	sources := f.query(args(), "connectors.sources", map[string]any{})
	eventually(t, "the holder shown", 3*time.Second, func() bool { return strings.Contains(sources.json(), `"adapter":"test-a"`) })
	// Polled again and again: event 3 has no cursor, so it is emitted each
	// pass, and dropped as a duplicate
	time.Sleep(2500 * time.Millisecond)
	a()
	b := f.serve(connectorstest.Options("b", nil))
	_ = b
	eventually(t, "the second process holding it", 5*time.Second, func() bool { return strings.Contains(sources.json(), `"adapter":"test-b"`) })
	time.Sleep(1500 * time.Millisecond)
	got := f.hits.all()
	if len(got) != 3 {
		t.Fatalf("hits %v, want j-1, j-2, j-3 once each", got)
	}
	// The cursor kept: more events reach the trigger, each once
	res := f.ok("connection.update", c.ID, map[string]any{"settings": map[string]any{"events": 5, "loseCursorFrom": nil}})
	f.connection(c.ID, res.Seq)
	eventually(t, "five hits", 6*time.Second, func() bool { return len(f.hits.all()) >= 5 })
	time.Sleep(1500 * time.Millisecond)
	seen := map[any]int{}
	for _, h := range f.hits.all() {
		seen[h]++
	}
	for i := 1; i <= 5; i++ {
		if seen[fmt.Sprintf("j-%d", i)] != 1 {
			t.Errorf("j-%d reached the trigger %d times: %v", i, seen[fmt.Sprintf("j-%d", i)], f.hits.all())
		}
	}
	// Paused, it is listed no more
	f.ok("connection.update", c.ID, map[string]any{"options": map[string]any{"paused": true, "pollInterval": "1s"}})
	eventually(t, "the source gone", 3*time.Second, func() bool { _, ok := f.source(c.ID); return !ok })
}

// An exclusive type served by two adapter instances never has two
// sessions open on one connection.
func TestExclusive(t *testing.T) {
	f := setup(t)
	f.serve(connectorstest.Options("a", nil))
	f.serve(connectorstest.Options("b", nil))
	f.online("test.exclusive")
	c := f.create(map[string]any{"type": "test.exclusive", "name": "Mailbox", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": "p"}})
	var wg sync.WaitGroup
	var failed atomic.Int64
	by := sync.Map{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := f.call(args(), c.ID, "echo", `{"text":"x"}`)
			if e != nil {
				t.Log(e)
				failed.Add(1)
				return
			}
			by.Store(out["by"], true)
		}()
	}
	wg.Wait()
	if failed.Load() > 0 {
		t.Errorf("%d calls failed", failed.Load())
	}
	if n := connectorstest.Sessions.Max(c.ID); n != 1 {
		t.Errorf("%d sessions were open at once on an exclusive connection", n)
	}
}

// A ledger hit returns the stored result with no request on the bus, also
// when the first send ran in another process.
func TestLedger(t *testing.T) {
	f := setup(t)
	var served connectorstest.Counter
	f.serve(connectorstest.Options("a", &served))
	f.online("test.secure")
	c := f.create(map[string]any{"type": "test.secure", "name": "Jobs", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": "p"}})
	out, e := f.call(args(), c.ID, "start", `{"id":"k1","text":"export"}`)
	if e != nil || out["jobId"] != "j-k1" || served.N() != 1 {
		t.Fatalf("the first start: %v %v (%d served)", out, e, served.N())
	}
	// Another process: its own NATS connection and dispatcher
	nc, err := f.srv.Connect("other")
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	var calls atomic.Int64
	tap, _ := f.srv.Connect("tap")
	defer tap.Close()
	_, _ = tap.Subscribe("cx.call.>", func(*nats.Msg) { calls.Add(1) })
	_ = tap.Flush()
	other := &connectors.Dispatcher{NC: nc, Registry: f.deps.Registry, Connections: connections.Connections{Deps: func() connections.Deps { return f.deps }}, Timeout: 3 * time.Second}
	raw, e := other.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: "start", Connection: c.ID, Args: json.RawMessage(`{"id":"k1","text":"export"}`)})
	time.Sleep(100 * time.Millisecond)
	if e != nil || !strings.Contains(string(raw), "j-k1") || served.N() != 1 || calls.Load() != 0 {
		t.Errorf("the retry: %s %v (%d served, %d calls on the bus)", raw, e, served.N(), calls.Load())
	}
	if _, e := other.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: "start", Connection: c.ID, Args: json.RawMessage(`{"id":"k2"}`)}); e != nil || served.N() != 2 {
		t.Errorf("another key: %v (%d served)", e, served.N())
	}
}

// An auth error from a call and from an event source each mark the
// connection errored and stop its source; connection.update starts it
// again.
func TestAuthStatus(t *testing.T) {
	f := setup(t)
	f.keep()
	f.serve(connectorstest.Options("a", nil))
	f.online("test.secure")
	c := f.create(map[string]any{"type": "test.secure", "name": "Inbox", "owner": "tenant", "settings": map[string]any{"user": "u"},
		"secrets": map[string]any{"password": "wrong"}, "options": map[string]any{"pollInterval": "1s"}})
	// The call: its session's sign-in is refused
	if _, e := f.call(args(), c.ID, "echo", `{"text":"x"}`); e == nil || e.Code != connectors.CodeAuth {
		t.Fatalf("a refused password: %v", e)
	}
	status := func(want string) {
		t.Helper()
		eventually(t, "status "+want, 3*time.Second, func() bool {
			got, _, _ := connections.Get(context.Background(), f.p, 1, c.ID)
			return got.Status == want
		})
	}
	status(connections.StatusError)
	f.installWatcher("")
	time.Sleep(300 * time.Millisecond)
	if _, ok := f.source(c.ID); ok {
		t.Errorf("an errored connection has a source")
	}
	if _, e := f.call(args(), c.ID, "echo", `{"text":"x"}`); e == nil || e.Retryable() {
		t.Errorf("a call on an errored connection: %v", e)
	}
	// Updated with a good password, it runs again; its source then fails
	// to sign in on its own
	res := f.ok("connection.update", c.ID, map[string]any{"secrets": map[string]any{"password": "good"}, "settings": map[string]any{"failWatch": "auth"}})
	if got := f.connection(c.ID, res.Seq); got.Status != connections.StatusOK {
		t.Errorf("an update did not clear the error: %+v", got)
	}
	eventually(t, "the source's auth error recorded", 5*time.Second, func() bool {
		got, _, _ := connections.Get(context.Background(), f.p, 1, c.ID)
		return got.Status == connections.StatusError && strings.Contains(got.LastError, "as asked")
	})
	eventually(t, "the source stopped again", 3*time.Second, func() bool { _, ok := f.source(c.ID); return !ok })
	res = f.ok("connection.update", c.ID, map[string]any{"settings": map[string]any{"failWatch": nil}})
	if got := f.connection(c.ID, res.Seq); got.Status != connections.StatusOK {
		t.Errorf("an update did not clear the error: %+v", got)
	}
	eventually(t, "the source back", 3*time.Second, func() bool { _, ok := f.source(c.ID); return ok })
	status(connections.StatusOK)
	// connection.test records ok
	if res, err := connections.Test(args(), f.deps, c.ID); err != nil || !res.OK {
		t.Errorf("connection.test: %+v %v", res, err)
	}
}

// Each connection command's inverse restores the connection, and undoing
// the undo makes it again (forge 06a's property, for 07c's commands).
func TestEveryOpUndoes(t *testing.T) {
	f := setup(t)
	f.serve(connectorstest.Options("a", nil))
	f.online("test.secure")
	c := f.create(map[string]any{"type": "test.secure", "name": "Undo", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": "p"}})
	state := func(seq uint64) string {
		got := f.connection(c.ID, seq)
		got.UpdatedAt, got.CheckedAt = time.Time{}, time.Time{}
		raw, _ := json.Marshal(got)
		return string(raw)
	}
	opOf := func(seq uint64) *live.LoggedOp {
		stream, _ := f.p.Stream(context.Background(), 1, live.AreaDefinitions)
		m, err := stream.GetMsg(context.Background(), seq)
		if err != nil {
			t.Fatal(err)
		}
		var e live.Event
		_ = json.Unmarshal(m.Data, &e)
		return e.Op
	}
	var seq uint64 = 1
	for _, st := range []struct {
		command string
		args    map[string]any
	}{
		{"connection.update", map[string]any{"settings": map[string]any{"user": "v", "events": 2}}},
		{"connection.update", map[string]any{"secrets": map[string]any{"password": "q"}}},
		{"connection.update", map[string]any{"options": map[string]any{"pollInterval": "5m", "since": "2026-01-01"}}},
		{"connection.rename", map[string]any{"name": "Renamed"}},
		{"connection.disable", map[string]any{}},
		{"connection.enable", map[string]any{}},
	} {
		before := state(seq)
		res := f.ok(st.command, c.ID, st.args)
		seq = res.Seq
		after := state(seq)
		op := opOf(res.Seq)
		if op == nil || len(op.Inverse) == 0 {
			t.Fatalf("%s: no inverse", st.command)
		}
		for _, inv := range op.Inverse {
			var a map[string]any
			_ = json.Unmarshal(inv.Args, &a)
			seq = f.ok(inv.Command, inv.Target, a).Seq
		}
		if got := state(seq); got != before {
			t.Errorf("%s: undoing gave\n  %s\nnot\n  %s", st.command, got, before)
		}
		var a map[string]any
		_ = json.Unmarshal(op.Inverse[0].Args, &a)
		_ = a
		seq = f.ok(st.command, c.ID, st.args).Seq
		if got := state(seq); st.command != "connection.update" || !strings.Contains(fmt.Sprint(st.args), "password") {
			if got != after {
				t.Errorf("%s: doing it again gave\n  %s\nnot\n  %s", st.command, got, after)
			}
		}
	}
	res := f.ok("connection.delete", c.ID, map[string]any{})
	if op := opOf(res.Seq); op == nil || op.Irreversible == "" {
		t.Errorf("delete is not irreversible: %+v", op)
	}
}

var (
	_ = adapter.Options{}
	_ = jetstream.KeyValueConfig{}
)
