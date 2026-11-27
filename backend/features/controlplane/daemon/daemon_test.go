package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type env struct {
	s      *natsauth.TestServer
	ctl    *ctl.Control
	enroll string // the enroll user's creds file
}

func setup(t *testing.T) *env {
	t.Helper()
	s := natsauth.StartTestSecure(t)
	nc, err := s.Conn(natsauth.Control, "serve", natsauth.Everything)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, _ := jetstream.New(nc)
	c := &ctl.Control{NC: nc, JS: js}
	ctx := context.Background()
	if err := c.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.ServeEnroll(ctx, c, s.D.Account(natsauth.Control)); err != nil {
		t.Fatal(err)
	}
	enroll, _ := s.CredsFile(natsauth.Control, "enroll", ctl.EnrollPermissions, 0)
	return &env{s: s, ctl: c, enroll: enroll}
}

func (e *env) enrolled(t *testing.T, target ctl.Target) Config {
	t.Helper()
	tok, err := e.ctl.MintToken(context.Background(), target, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c, err := Enroll(context.Background(), Config{Mode: "host", NatsURLs: []string{e.s.URL}, State: dir}, e.enroll, tok, filepath.Join(dir, "axiomd.json"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEnrollOnce(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	h1 := ctl.Target{Kind: ctl.KindHost, ID: "h1"}
	tok, err := e.ctl.MintToken(ctx, h1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "axiomd.json")
	c, err := Enroll(ctx, Config{Mode: "host", NatsURLs: []string{e.s.URL}, State: dir}, e.enroll, tok, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.Target != "host.h1" {
		t.Fatalf("target %q", c.Target)
	}
	if saved, err := LoadConfig(cfgPath); err != nil || saved.Creds == "" {
		t.Fatalf("config %+v %v", saved, err)
	}
	if _, err := Enroll(ctx, Config{NatsURLs: []string{e.s.URL}, State: t.TempDir()}, e.enroll, tok, ""); err == nil || !strings.Contains(err.Error(), "used") {
		t.Fatalf("a spent token enrolled again: %v", err)
	}
	old, _ := e.ctl.MintToken(ctx, h1, -time.Second)
	if _, err := Enroll(ctx, Config{NatsURLs: []string{e.s.URL}, State: t.TempDir()}, e.enroll, old, ""); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("an expired token enrolled: %v", err)
	}
	if _, err := Enroll(ctx, Config{NatsURLs: []string{e.s.URL}, State: t.TempDir()}, e.enroll, "nonsense", ""); err == nil {
		t.Fatal("an unknown token enrolled")
	}
}

// TestDaemonCredsAreScoped: an enrolled daemon reads its own desired
// state and reports, and cannot listen to another daemon nor read a
// tenant stream.
func TestDaemonCredsAreScoped(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.enrolled(t, ctl.Target{Kind: ctl.KindHost, ID: "h1"})
	d := &Daemon{Config: c}
	if err := d.Connect(nil); err != nil {
		t.Fatal(err)
	}
	defer d.Control().NC.Close()
	h1 := ctl.Target{Kind: ctl.KindHost, ID: "h1"}
	if _, err := e.ctl.PutDesired(ctx, h1, json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if raw, ok, err := d.Control().Desired(ctx, h1); err != nil || !ok || string(raw) != `{"a":1}` {
		t.Fatalf("own desired state: %s %v %v", raw, ok, err)
	}
	if err := d.Control().PutFacts(ctx, h1, map[string]any{"os": "x"}); err != nil {
		t.Fatalf("own facts: %v", err)
	}
	if err := d.Control().Report(ctx, ctl.Event{Request: "r", Kind: ctl.EventDone, Target: h1}); err != nil {
		t.Fatalf("own events: %v", err)
	}
	tctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	h2 := ctl.Target{Kind: ctl.KindHost, ID: "h2"}
	if _, ok, err := d.Control().Desired(tctx, h2); err == nil && ok {
		t.Fatal("read another daemon's desired state")
	}
	if err := d.Control().Report(tctx, ctl.Event{Request: "r", Kind: ctl.EventDone, Target: h2}); err == nil {
		t.Fatal("reported as another daemon")
	}
	errs := make(chan error, 4)
	d.Control().NC.SetErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) { errs <- err })
	if _, err := d.Control().NC.SubscribeSync(h2.Requests()); err != nil {
		t.Fatal(err)
	}
	_ = d.Control().NC.Flush()
	select {
	case err := <-errs:
		if !strings.Contains(strings.ToLower(err.Error()), "permissions violation") {
			t.Fatalf("subscribing to another daemon: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscribed to another daemon's requests")
	}
	if _, err := d.Control().JS.Stream(tctx, "EVT_1_def"); err == nil {
		t.Fatal("read a tenant stream")
	}
}

func TestReconcileAndRequests(t *testing.T) {
	e := setup(t)
	h1 := ctl.Target{Kind: ctl.KindHost, ID: "h1"}
	c := e.enrolled(t, h1)
	// Desired state changed while the daemon was stopped
	if _, err := e.ctl.PutDesired(context.Background(), h1, json.RawMessage(`{"v":2}`)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []string
	release := make(chan struct{})
	slow := func(ctx context.Context, r ctl.Request, progress func(string)) error {
		progress("working")
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	start := func(h Handler) (context.CancelFunc, *Daemon) {
		ctx, cancel := context.WithCancel(context.Background())
		d := &Daemon{Config: c, Handlers: map[string]Handler{"slow": h}, Beat: 200 * time.Millisecond,
			Reconcile: func(_ context.Context, desired json.RawMessage) error {
				mu.Lock()
				seen = append(seen, string(desired))
				mu.Unlock()
				return nil
			},
			Facts: func(context.Context) map[string]any { return map[string]any{"os": "test"} }}
		if err := d.Connect(nil); err != nil {
			t.Fatal(err)
		}
		go func() { _ = d.Run(ctx) }()
		return func() { cancel(); time.Sleep(100 * time.Millisecond); d.Control().NC.Close() }, d
	}
	stop, _ := start(slow)
	until(t, "the desired state is applied on start", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) > 0 && seen[len(seen)-1] == `{"v":2}`
	})
	until(t, "facts and heartbeat", func() bool {
		_, up, _ := e.ctl.Health(context.Background(), h1)
		f, ok, _ := e.ctl.Facts(context.Background(), h1)
		return up && ok && strings.Contains(string(f), `"axiomd"`)
	})
	// Ten identical requests: one done
	for i := 0; i < 10; i++ {
		if _, err := e.ctl.Send(context.Background(), h1, ctl.Request{ID: "same", Verb: "reconcile"}); err != nil {
			t.Fatal(err)
		}
	}
	until(t, "one done for ten", func() bool { return count(t, e, "same", ctl.EventDone) == 1 })
	time.Sleep(300 * time.Millisecond)
	if n := count(t, e, "same", ctl.EventDone); n != 1 {
		t.Fatalf("%d done events", n)
	}
	// Killed mid-request: finished on restart
	if _, err := e.ctl.Send(context.Background(), h1, ctl.Request{ID: "long", Verb: "slow"}); err != nil {
		t.Fatal(err)
	}
	until(t, "the slow request reports progress", func() bool { return count(t, e, "long", ctl.EventProgress) == 1 })
	stop()
	if count(t, e, "long", ctl.EventDone) != 0 {
		t.Fatal("done before it finished")
	}
	close(release)
	stop2, _ := start(slow)
	defer stop2()
	until(t, "the interrupted request is finished on restart", func() bool { return count(t, e, "long", ctl.EventDone) == 1 })
}

func count(t *testing.T, e *env, request, kind string) int {
	st, err := e.ctl.JS.Stream(context.Background(), ctl.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for seq := uint64(1); ; seq++ {
		m, err := st.GetMsg(context.Background(), seq)
		if err != nil {
			info, _ := st.Info(context.Background())
			if info != nil && seq < info.State.LastSeq {
				continue
			}
			return n
		}
		var ev ctl.Event
		_ = json.Unmarshal(m.Data, &ev)
		if ev.Request == request && ev.Kind == kind {
			n++
		}
	}
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting until %s", what)
}
