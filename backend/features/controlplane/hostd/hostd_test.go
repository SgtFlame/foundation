package hostd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/daemon"
	"github.com/SgtFlame/foundation/backend/features/controlplane/lxd/lxdtest"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/nats-io/nats.go/jetstream"
)

type env struct {
	s      *natsauth.TestServer
	ctl    *ctl.Control
	enroll string
	lxd    *lxdtest.Server
	cfg    daemon.Config
	host   ctl.Target
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
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := c.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.ServeEnroll(ctx, c, s.D.Account(natsauth.Control)); err != nil {
		t.Fatal(err)
	}
	enroll, _ := s.CredsFile(natsauth.Control, "enroll", ctl.EnrollPermissions, 0)
	e := &env{s: s, ctl: c, enroll: enroll, lxd: lxdtest.Start(t), host: ctl.Target{Kind: ctl.KindHost, ID: "h1"}}
	tok, err := c.MintToken(ctx, e.host, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	e.cfg, err = daemon.Enroll(ctx, daemon.Config{Mode: "host", NatsURLs: []string{s.URL}, State: dir}, enroll, tok, filepath.Join(dir, "axiomd.json"))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// desire writes the host's desired state and each instance's (as the
// control plane's follower does), so minting checks placement.
func (e *env) desire(t *testing.T, insts ...Instance) {
	t.Helper()
	ctx := context.Background()
	doc := map[string]any{"instances": insts, "profiles": map[string]Profile{"p1": {ID: "p1", Name: "serve", Kind: "container", Image: "ubuntu/24.04", CPU: 2, Memory: "2GiB"}}}
	raw, _ := json.Marshal(doc)
	if _, err := e.ctl.PutDesired(ctx, e.host, raw); err != nil {
		t.Fatal(err)
	}
	for _, i := range insts {
		raw, _ := json.Marshal(map[string]any{"instance": map[string]any{"name": i.Name, "host": e.host.ID, "status": i.Status}})
		if _, err := e.ctl.PutDesired(ctx, ctl.Target{Kind: ctl.KindInstance, ID: i.ID}, raw); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *env) start(t *testing.T) (*daemon.Daemon, context.CancelFunc) {
	t.Helper()
	h := &Host{LXD: e.lxd.Client(), KVM: func() bool { return false }}
	d := h.Daemon(e.cfg)
	d.Beat = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Connect(nil); err != nil {
		t.Fatal(err)
	}
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() { cancel(); d.Control().NC.Close() })
	return d, cancel
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// outcome waits for a request's done or failed event on CTL.
func (e *env) outcome(t *testing.T, id string) ctl.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cons, err := e.ctl.JS.OrderedConsumer(ctx, ctl.StreamName, jetstream.OrderedConsumerConfig{FilterSubjects: []string{e.host.EventSubject("*")}})
	if err != nil {
		t.Fatal(err)
	}
	for {
		m, err := cons.Next(jetstream.FetchMaxWait(10 * time.Second))
		if err != nil {
			t.Fatalf("no outcome for %s: %v", id, err)
		}
		var ev ctl.Event
		_ = json.Unmarshal(m.Data(), &ev)
		if ev.Request == id && (ev.Kind == ctl.EventDone || ev.Kind == ctl.EventFailed) {
			return ev
		}
	}
}

func (e *env) send(t *testing.T, id, verb string, args any) {
	t.Helper()
	raw, _ := json.Marshal(args)
	var rep ctl.Reply
	var err error
	for try := 0; try < 50; try++ { // the daemon subscribes as it starts
		rep, err = e.ctl.Send(context.Background(), e.host, ctl.Request{ID: id, Verb: verb, Args: raw})
		if !errors.Is(err, ctl.ErrNoDaemon) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || !rep.Accepted {
		t.Errorf("%s: %+v %v", verb, rep, err)
	}
}

// TestCreatedOnce: a desired instance is made once though reconcile is
// asked for ten times at once, with its profile, its user key, its
// cluster target and a token its agent exchanges for its own creds.
func TestCreatedOnce(t *testing.T) {
	e := setup(t)
	e.lxd.Clustered, e.lxd.Name = true, "member1"
	e.lxd.SlowCreate = 300 * time.Millisecond
	e.desire(t, Instance{ID: "i1", Name: "Serve 1", Profile: "p1"})
	e.start(t)
	var wg sync.WaitGroup
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			e.send(t, "rec-"+string(rune('a'+n)), "reconcile", nil)
		}(n)
	}
	wg.Wait()
	until(t, "the instance runs", func() bool { i, ok := e.lxd.Instances()["serve-1"]; return ok && i.Running() })
	time.Sleep(300 * time.Millisecond)
	if n := e.lxd.Creates(); n != 1 {
		t.Fatalf("%d creates", n)
	}
	if tg := e.lxd.Targets(); tg[0] != "member1" {
		t.Fatalf("created on %v", tg)
	}
	in := e.lxd.Instances()["serve-1"]
	if in.Config[KeyID] != "i1" || len(in.Profiles) != 2 || in.Profiles[1] != "axiom-p1" {
		t.Fatalf("instance %+v", in)
	}
	if p := e.lxd.Profiles()["axiom-p1"]; p.Config["limits.cpu"] != "2" || p.Config["limits.memory"] != "2GiB" {
		t.Fatalf("profile %+v", p)
	}
	if ud := in.Config["cloud-init.user-data"]; !strings.Contains(ud, "axiomd enroll --from /etc/axiomd/enroll.json") || !strings.Contains(ud, "axiomd-agent.service") {
		t.Fatalf("the user data does not start the agent:\n%s", ud)
	}
	f, err := ReadUserData(in.Config["cloud-init.user-data"])
	if err != nil {
		t.Fatal(err)
	}
	if f.Token == "" || f.EnrollCreds == "" || f.Mode != "agent" {
		t.Fatalf("enrollment file %+v", f)
	}
	dir := t.TempDir()
	raw, _ := json.Marshal(f)
	path := filepath.Join(dir, "enroll.json")
	if err := writeFile(path, raw); err != nil {
		t.Fatal(err)
	}
	c, err := daemon.EnrollFrom(context.Background(), path, filepath.Join(dir, "state"), filepath.Join(dir, "axiomd.json"))
	if err != nil {
		t.Fatalf("the agent cannot enroll: %v", err)
	}
	if c.Target != "inst.i1" {
		t.Fatalf("enrolled as %s", c.Target)
	}
}

// TestAdoptsHalfMade: a daemon killed mid-create adopts the instance on
// restart instead of making it again.
func TestAdoptsHalfMade(t *testing.T) {
	e := setup(t)
	e.lxd.SlowCreate = 2 * time.Second
	e.desire(t, Instance{ID: "i1", Name: "web", Profile: "p1"})
	stop := make(chan context.CancelFunc, 1)
	crashed := make(chan struct{})
	e.lxd.Crash = func() { (<-stop)(); close(crashed) }
	_, cancel := e.start(t)
	stop <- cancel
	<-crashed
	if _, ok := e.lxd.Instances()["web"]; !ok {
		t.Fatal("no half-made instance")
	}
	// The half-made instance is stopped: the restarted daemon adopts and starts it
	e.lxd.Set(func(s *lxdtest.Server) { s.SlowCreate = 0 })
	e.start(t)
	until(t, "the adopted instance runs", func() bool { return e.lxd.Instances()["web"].Running() })
	if n := e.lxd.Creates(); n != 1 {
		t.Fatalf("%d creates", n)
	}
}

// TestVerbs: stop, snapshot (once by name), restore, and delete refused
// while the instance is desired, done once it is absent.
func TestVerbs(t *testing.T) {
	e := setup(t)
	e.desire(t, Instance{ID: "i1", Name: "db", Profile: "p1", Status: "running"})
	e.start(t)
	until(t, "the instance runs", func() bool { return e.lxd.Instances()["db"].Running() })
	e.send(t, "snap1", "snapshot", args{Instance: "i1", Name: "before"})
	e.send(t, "snap2", "snapshot", args{Instance: "i1", Name: "before"})
	if ev := e.outcome(t, "snap1"); ev.Kind != ctl.EventDone {
		t.Fatalf("snapshot: %+v", ev)
	}
	if ev := e.outcome(t, "snap2"); ev.Kind != ctl.EventDone {
		t.Fatalf("snapshot again: %+v", ev)
	}
	if s := e.lxd.Snapshots("db"); len(s) != 1 {
		t.Fatalf("snapshots %v", s)
	}
	e.send(t, "restore1", "restore", args{Instance: "i1", Name: "before"})
	if ev := e.outcome(t, "restore1"); ev.Kind != ctl.EventDone {
		t.Fatalf("restore: %+v", ev)
	}
	e.send(t, "del1", "delete", args{Instance: "i1"})
	if ev := e.outcome(t, "del1"); ev.Kind != ctl.EventFailed || !strings.Contains(ev.Message, "absent") {
		t.Fatalf("delete while desired: %+v", ev)
	}
	e.send(t, "stop1", "stop", args{Instance: "i1"})
	if ev := e.outcome(t, "stop1"); ev.Kind != ctl.EventDone || e.lxd.Instances()["db"].Running() {
		t.Fatalf("stop: %+v", ev)
	}
	e.desire(t, Instance{ID: "i1", Name: "db", Profile: "p1", Status: "absent"})
	e.send(t, "rec1", "reconcile", nil)
	until(t, "the absent instance is deleted", func() bool { _, ok := e.lxd.Instances()["db"]; return !ok })
}

// TestCapabilities: vms only when LXD has qemu and KVM opens.
func TestCapabilities(t *testing.T) {
	l := lxdtest.Start(t)
	for _, c := range []struct {
		driver string
		kvm    bool
		want   string
	}{{"lxc | qemu", true, "containers,vms"}, {"lxc | qemu", false, "containers"}, {"lxc", true, "containers"}} {
		l.Driver = c.driver
		h := &Host{LXD: l.Client(), KVM: func() bool { return c.kvm }}
		f := h.Facts(context.Background())
		caps, _ := f["capabilities"].([]string)
		if got := strings.Join(caps, ","); got != c.want {
			t.Fatalf("%s kvm=%v: %s", c.driver, c.kvm, got)
		}
		if lx, _ := f["lxd"].(map[string]any); lx["version"] != "6.1" {
			t.Fatalf("lxd facts %v", f["lxd"])
		}
	}
}

// TestMintOnlyForOwnInstances: a host gets no token for an instance
// placed on another host.
func TestMintOnlyForOwnInstances(t *testing.T) {
	e := setup(t)
	raw, _ := json.Marshal(map[string]any{"instance": map[string]any{"host": "h2"}})
	if _, err := e.ctl.PutDesired(context.Background(), ctl.Target{Kind: ctl.KindInstance, ID: "other"}, raw); err != nil {
		t.Fatal(err)
	}
	d, _ := e.start(t)
	if _, err := d.Control().Mint(context.Background(), "h1", "other"); err == nil {
		t.Fatal("minted a token for another host's instance")
	}
	tctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := d.Control().Mint(tctx, "h2", "other"); err == nil {
		t.Fatal("asked as another host")
	}
}

func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o600) }
