package connectors_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// The external adapter: the test binary run again with CX_HELPER_URL
// serves the test types on that NATS server until it is killed.
func TestMain(m *testing.M) {
	if url := os.Getenv("CX_HELPER_URL"); url != "" {
		os.Exit(helper(url))
	}
	os.Exit(m.Run())
}

func helper(url string) int {
	nc, err := nats.Connect(url, nats.Name("external-adapter"))
	if err != nil {
		fmt.Println("connect:", err)
		return 1
	}
	o := connectorstest.Options("ext", nil)
	o.AnnounceEvery, o.TTL = 100*time.Millisecond, 400*time.Millisecond
	go func() {
		time.Sleep(200 * time.Millisecond)
		fmt.Println("ready")
	}()
	if err := adapter.Serve(context.Background(), nc, o); err != nil {
		fmt.Println("serve:", err)
		return 1
	}
	return 0
}

type fixture struct {
	t   *testing.T
	srv *natsd.Server
	reg *extensions.Registry
	r   *connectors.Registry
	d   *connectors.Dispatcher
}

func setup(t *testing.T, srv *natsd.Server) *fixture {
	if srv == nil {
		srv = natsd.StartTest(t)
	}
	reg := extensions.NewRegistry()
	reg.Declare(connectors.Contracts, connectors.Types)
	connectorstest.Contribute(reg)
	r := connectors.NewRegistry(reg, srv.JS())
	r.SetCheckEvery(50 * time.Millisecond)
	t.Cleanup(r.Close)
	return &fixture{t: t, srv: srv, reg: reg, r: r, d: &connectors.Dispatcher{NC: srv.Conn(), Registry: r, Timeout: 3 * time.Second}}
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

// serve starts an adapter on its own connection, as another process would;
// stop ends it and waits.
func (f *fixture) serve(o adapter.Options) (stop func()) {
	f.t.Helper()
	nc, err := f.srv.Connect(o.Name)
	if err != nil {
		f.t.Fatal(err)
	}
	if o.AnnounceEvery == 0 {
		o.AnnounceEvery, o.TTL = 100*time.Millisecond, 400*time.Millisecond
	}
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

// online waits until type typ's online state is want.
func (f *fixture) online(typ string, want bool, within time.Duration) connectors.TypeInfo {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for {
		ti, ok, err := f.r.Type(context.Background(), 1, typ)
		if err != nil {
			f.t.Fatal(err)
		}
		if ok && ti.Online == want {
			return ti
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("%s is not online=%v: %+v", typ, want, ti)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (f *fixture) echo(text string) (map[string]any, *connectors.Error) {
	raw, e := f.d.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: "echo", Args: json.RawMessage(`{"text":"` + text + `"}`)})
	if e != nil {
		return nil, e
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

// An adapter in this process: the registry describes the test contract
// and its types, a call returns the result, and a typed error comes back
// as it was returned, retryable or not.
func TestInProcessAdapter(t *testing.T) {
	f := setup(t, nil)
	if ti := f.online("test.echo", false, time.Second); ti.Source != connectors.FromBuiltin {
		t.Errorf("a builtin type with no adapter: %+v", ti)
	}
	f.serve(connectorstest.Options("a", nil))
	f.online("test.echo", true, 2*time.Second)
	d, err := f.r.Describe(context.Background(), 1, "contract:test.echo@1")
	if err != nil || len(d["types"].([]connectors.TypeInfo)) != 3 || d["online"] != true {
		t.Fatalf("describe: %v %v", d, err)
	}
	if d, err := f.r.Describe(context.Background(), 1, "contract:test.echo"); err != nil || d["contract"].(connectors.Contract).Version != 1 {
		t.Errorf("the latest version: %v %v", d, err)
	}
	out, e := f.echo("hi")
	if e != nil || out["text"] != "hi" || out["length"] != float64(2) || out["by"] != "a" {
		t.Fatalf("echo: %v %v", out, e)
	}
	_, e = f.d.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: "fail", Args: json.RawMessage(`{"code":"notFound","message":"gone"}`)})
	if e == nil || e.Code != "notFound" || e.Message != "gone" || e.Retryable() {
		t.Errorf("notFound: %+v", e)
	}
	_, e = f.d.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: "fail", Args: json.RawMessage(`{"code":"rateLimited"}`)})
	if e == nil || e.Code != "rateLimited" || !e.Retryable() {
		t.Errorf("rateLimited: %+v", e)
	}
	_, e = f.d.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: "echo", Args: json.RawMessage(`{"repeat":2}`)})
	if e == nil || e.Code != "invalid" {
		t.Errorf("args without text: %+v", e)
	}
	_, e = f.d.Call(args(), connectors.Call{Contract: "test.nope@1", Operation: "echo"})
	if e == nil || e.Code != "notSupported" {
		t.Errorf("an unknown contract: %+v", e)
	}
}

// With no adapter a call is unavailable at once, and retryable; once one
// starts, the same call succeeds.
func TestNoAdapter(t *testing.T) {
	f := setup(t, nil)
	start := time.Now()
	_, e := f.echo("x")
	if e == nil || e.Code != connectors.CodeUnavailable || !e.Retryable() || time.Since(start) > time.Second {
		t.Fatalf("no adapter: %+v after %s", e, time.Since(start))
	}
	f.serve(connectorstest.Options("late", nil))
	f.online("test.echo", true, 2*time.Second)
	if out, e := f.echo("x"); e != nil || out["by"] != "late" {
		t.Errorf("after an adapter started: %v %v", out, e)
	}
}

// Two instances of one type share its calls, and calls go on when one
// stops: a call caught by the stop is retried (as a job would) and served
// by the other, so none is lost.
func TestTwoInstances(t *testing.T) {
	f := setup(t, nil)
	var a, b connectorstest.Counter
	stopA := f.serve(connectorstest.Options("a", &a))
	f.serve(connectorstest.Options("b", &b))
	if ti := f.online("test.echo", true, 2*time.Second); ti.Instances < 1 {
		t.Fatalf("%+v", ti)
	}
	deadline := time.Now().Add(2 * time.Second)
	for ti, _, _ := f.r.Type(context.Background(), 1, "test.echo"); ti.Instances != 2; ti, _, _ = f.r.Type(context.Background(), 1, "test.echo") {
		if time.Now().After(deadline) {
			t.Fatalf("instances: %+v", ti)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i := range 40 {
		if _, e := f.echo(fmt.Sprint(i)); e != nil {
			t.Fatal(e)
		}
	}
	if a.N() == 0 || b.N() == 0 || a.N()+b.N() != 40 {
		t.Fatalf("shared: a %d, b %d", a.N(), b.N())
	}
	var served atomic.Int64
	var wg sync.WaitGroup
	for i := range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for try := 0; try < 50; try++ {
				if _, e := f.echo(fmt.Sprint("c", i)); e == nil {
					served.Add(1)
					return
				} else if !e.Retryable() {
					t.Errorf("call %d: %v", i, e)
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Errorf("call %d was never served", i)
		}()
		if i == 20 {
			stopA()
		}
	}
	wg.Wait()
	if served.Load() != 60 {
		t.Errorf("served %d of 60", served.Load())
	}
}

// The same type served by an adapter in another process, over TCP: calls
// reach it, its announcement lists the type, and the type goes offline
// when the process is killed.
func TestExternalAdapter(t *testing.T) {
	srv := natsd.StartTestListening(t)
	f := setup(t, srv)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "CX_HELPER_URL="+srv.ClientURL())
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	lines := bufio.NewScanner(stdout)
	if !lines.Scan() || lines.Text() != "ready" {
		t.Fatalf("the helper said %q", lines.Text())
	}
	f.online("test.echo", true, 3*time.Second)
	if out, e := f.echo("far"); e != nil || out["by"] != "ext" {
		t.Fatalf("external echo: %v %v", out, e)
	}
	_ = cmd.Process.Kill() // no goodbye: its announcement must expire
	_ = cmd.Wait()
	f.online("test.echo", false, 3*time.Second)
	if _, e := f.echo("gone"); e == nil || e.Code != connectors.CodeUnavailable {
		t.Errorf("after the process died: %+v", e)
	}
}

// syncBuffer is a log sink tests read.
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

// A call on a connection whose settings hold a secret: the session opens
// with it, but no message on the bus and no log line carries it, even
// when the call fails.
func TestNoSecretOnTheBus(t *testing.T) {
	logs := &syncBuffer{}
	if err := logging.Setup(logs, "json", slog.LevelDebug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logging.Setup(os.Stderr, "text", slog.LevelInfo) })
	const secret = "hunter2-very-secret"
	f := setup(t, nil)
	f.d.Connections = connectors.ConnectionsFunc(func(_ features.ServiceArgs, conn string) (string, error) {
		if conn != "c1" {
			return "", fmt.Errorf("there is no connection %s", conn)
		}
		return "test.secure", nil
	})
	var traffic syncBuffer
	tap, err := f.srv.Connect("tap")
	if err != nil {
		t.Fatal(err)
	}
	defer tap.Close()
	if _, err := tap.Subscribe(">", func(m *nats.Msg) {
		fmt.Fprintf(&traffic, "%s %v %s\n", m.Subject, m.Header, m.Data)
	}); err != nil {
		t.Fatal(err)
	}
	_ = tap.Flush()
	o := connectorstest.Options("s", nil)
	var opened atomic.Bool
	o.Resolve = func(_ context.Context, tenant uint64, typ, conn string) (adapter.Connection, error) {
		opened.Store(true)
		return adapter.Connection{ID: conn, Type: typ, Tenant: tenant, Settings: json.RawMessage(`{"user":"u","password":"` + secret + `"}`)}, nil
	}
	f.serve(o)
	f.online("test.secure", true, 2*time.Second)
	call := func(op, a string) *connectors.Error {
		_, e := f.d.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: op, Connection: "c1", Args: json.RawMessage(a)})
		return e
	}
	if e := call("echo", `{"text":"hello"}`); e != nil || !opened.Load() {
		t.Fatalf("echo on c1: %v (opened %v)", e, opened.Load())
	}
	if e := call("fail", `{"code":"auth","message":"refused"}`); e == nil || e.Code != "auth" {
		t.Fatalf("fail on c1: %v", e)
	}
	if e := call("echo", `{"text":"x"}`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.d.Call(args(), connectors.Call{Contract: connectorstest.ContractRef, Operation: "echo", Connection: "c2", Args: json.RawMessage(`{"text":"x"}`)}); e == nil || e.Code != "notFound" {
		t.Errorf("an unknown connection: %v", e)
	}
	time.Sleep(100 * time.Millisecond)
	if !strings.Contains(traffic.String(), "cx.call.1.test_secure.c1.echo") {
		t.Fatalf("the tap saw no call:\n%s", traffic.String())
	}
	if strings.Contains(traffic.String(), secret) {
		t.Errorf("the secret crossed the bus:\n%s", traffic.String())
	}
	if strings.Contains(logs.String(), secret) {
		t.Errorf("the secret was logged:\n%s", logs.String())
	}
}

// The registry refuses an announcement whose metadata fails the check,
// and logs why; a good one from the same adapter is listed.
func TestRegistryRefusesBadAnnouncement(t *testing.T) {
	logs := &syncBuffer{}
	if err := logging.Setup(logs, "json", slog.LevelDebug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logging.Setup(os.Stderr, "text", slog.LevelInfo) })
	f := setup(t, nil)
	if _, err := f.r.Catalog(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	kv, err := connectors.AdaptersKV(context.Background(), f.srv.JS())
	if err != nil {
		t.Fatal(err)
	}
	bad := connectorstest.Contract()
	bad.ID, bad.Operations[0].Description = "test.bad", ""
	good := connectorstest.Contract()
	good.ID = "test.good"
	now := time.Now()
	for _, a := range []connectors.Announcement{
		{Adapter: "x", Instance: "i1", Type: &connectors.Type{ID: "test.bad", Contracts: []string{"test.bad@1"}, NoConnection: true, Source: "builtin",
			Meta: connectors.Meta{Title: "Bad", Description: "Undescribed operation", Tags: []string{"test"}}}, Contracts: []connectors.Contract{bad}, At: now, ExpiresAt: now.Add(time.Minute)},
		{Adapter: "x", Instance: "i1", Type: &connectors.Type{ID: "test.good", Contracts: []string{"test.good@1"}, NoConnection: true, Source: "builtin",
			Meta: connectors.Meta{Title: "Good", Description: "Fine", Tags: []string{"test"}}}, Contracts: []connectors.Contract{good}, At: now, ExpiresAt: now.Add(time.Minute)},
	} {
		if err := connectors.Announce(context.Background(), kv, a); err != nil {
			t.Fatal(err)
		}
	}
	f.online("test.good", true, 2*time.Second)
	if _, ok, _ := f.r.Type(context.Background(), 1, "test.bad"); ok {
		t.Error("the bad type is listed")
	}
	if _, ok, _ := f.r.Contract(context.Background(), 1, "test.bad@1"); ok {
		t.Error("the bad contract is listed")
	}
	if !strings.Contains(logs.String(), "announcement_refused") || !strings.Contains(logs.String(), "operations.echo.description") {
		t.Errorf("no log said why:\n%s", logs.String())
	}
}

// feed is a live.Feed for running a query in a test.
type feed struct {
	ctx    context.Context
	params json.RawMessage
	mu     sync.Mutex
	last   json.RawMessage
	got    chan struct{}
}

func newFeed(ctx context.Context, params string) *feed {
	return &feed{ctx: ctx, params: json.RawMessage(params), got: make(chan struct{}, 16)}
}

func (f *feed) Args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: f.ctx, Principal: features.LocalPrincipal()}
}
func (f *feed) Context() context.Context { return f.ctx }
func (f *feed) Params() json.RawMessage  { return f.params }
func (f *feed) Store() live.Store        { return nil }
func (f *feed) Snapshot(data any, _ live.Position) {
	raw, _ := json.Marshal(data)
	f.set(raw)
}
func (f *feed) Change(d live.Delta, _ live.Position) {
	if d.Op == "replace" {
		f.set(d.Value)
	}
}
func (f *feed) set(raw json.RawMessage) {
	f.mu.Lock()
	f.last = raw
	f.mu.Unlock()
	select {
	case f.got <- struct{}{}:
	default:
	}
}

// until waits until the query's value satisfies ok.
func (f *feed) until(t *testing.T, what string, ok func(v map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		f.mu.Lock()
		raw := f.last
		f.mu.Unlock()
		var v map[string]any
		if json.Unmarshal(raw, &v) == nil && ok(v) {
			return v
		}
		select {
		case <-f.got:
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("%s: last %s", what, raw)
		}
	}
}

func query(reg *extensions.Registry, id string) live.Query {
	for _, q := range live.Queries.All(reg) {
		if q.ID == id {
			return q
		}
	}
	panic("no query " + id)
}

// The registry's queries follow it: a surface an adapter serves (its
// Listen blocking until canceled) is listed as inbound while it runs, a
// type appears online when its adapter starts, and a discovered contract
// is described by its connection, its bad operation left out. Nothing is
// sent on cx.in.>.
func TestQueriesAndBothDirections(t *testing.T) {
	f := setup(t, nil)
	f.reg.Declare(live.Queries)
	connectors.Contribute(f.reg, func() connectors.Deps { return connectors.Deps{Registry: f.r, Dispatcher: f.d} })
	var inbound atomic.Int64
	if _, err := f.srv.Conn().Subscribe("cx.in.>", func(*nats.Msg) { inbound.Add(1) }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := func(id, params string) *feed {
		fd := newFeed(ctx, params)
		go func() { _ = query(f.reg, id).Run(fd) }()
		return fd
	}
	types := run("connectors.types", `{"direction":"inbound"}`)
	contracts := run("connectors.contracts", `{"search":"echo"}`)
	listening := make(chan struct{})
	o := adapter.Options{Name: "mcp", Surfaces: []adapter.Surface{{ID: "test.surface",
		Meta: connectors.Meta{Title: "Test surface", Description: "Listens nowhere", Tags: []string{"test"}},
		Listen: func(ctx context.Context, gw adapter.Gateway) error {
			if _, err := gw.Request(ctx, adapter.Credential{}, "x", "tools.list", nil); err == nil {
				t.Error("the gateway answered before iteration 15")
			}
			close(listening)
			<-ctx.Done()
			return nil
		}}}}
	f.serve(o)
	<-listening
	types.until(t, "the surface", func(v map[string]any) bool {
		s, _ := v["surfaces"].([]any)
		if len(s) != 1 {
			return false
		}
		e := s[0].(map[string]any)
		return e["direction"] == "inbound" && e["online"] == true && len(v["types"].([]any)) == 0
	})
	contracts.until(t, "the contract offline", func(v map[string]any) bool {
		c, _ := v["contracts"].([]any)
		return len(c) == 1 && c[0].(map[string]any)["online"] == false
	})
	f.serve(connectorstest.Options("q", nil))
	contracts.until(t, "the contract online", func(v map[string]any) bool {
		c, _ := v["contracts"].([]any)
		return len(c) == 1 && c[0].(map[string]any)["online"] == true
	})
	// A discovered contract: one good operation, one too long for a tool
	disc := connectorstest.Contract()
	disc.Events = nil
	disc.Operations = disc.Operations[:1]
	long := disc.Operations[0]
	long.ID = strings.Repeat("x", 70)
	disc.Operations = append(disc.Operations, long)
	if _, err := connectors.PutDiscovered(ctx, f.srv.JS(), 1, "42", disc); err != nil {
		t.Fatal(err)
	}
	describe := run("connectors.describe", `{"ref":"connection:42"}`)
	v := describe.until(t, "the discovered contract", func(v map[string]any) bool { return v["contract"] != nil })
	c := v["contract"].(map[string]any)
	if c["id"] != "conn.42" || len(c["operations"].([]any)) != 1 || len(v["problems"].([]any)) == 0 {
		t.Errorf("discovered: %v", v)
	}
	// The connection discovers again: the description follows
	again := connectorstest.Contract()
	again.Events = nil
	again.Operations = again.Operations[:2]
	if _, err := connectors.PutDiscovered(ctx, f.srv.JS(), 1, "42", again); err != nil {
		t.Fatal(err)
	}
	describe.until(t, "the rediscovered contract", func(v map[string]any) bool {
		c, _ := v["contract"].(map[string]any)
		return c != nil && len(c["operations"].([]any)) == 2 && len(v["problems"].([]any)) == 0
	})
	if info, ok, _ := f.r.Contract(ctx, 1, "conn.42"); !ok || len(info.Contract.Operations) != 2 {
		t.Errorf("conn.42: %+v", info)
	}
	if inbound.Load() != 0 {
		t.Errorf("%d messages on cx.in.>", inbound.Load())
	}
}

var _ jetstream.KeyValue // keeps the import for helpers above
