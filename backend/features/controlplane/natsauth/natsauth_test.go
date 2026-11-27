package natsauth

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestAccountsKeepSubjectsApart: a CTL user's ctl.> messages never reach
// an APP user subscribed to the same subjects, and an APP user's
// JetStream is its own.
func TestAccountsKeepSubjectsApart(t *testing.T) {
	s := StartTestSecure(t)
	ctl, err := s.Conn(Control, "ctl", Everything)
	if err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()
	app, err := s.Conn(App, "app", Everything)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var got atomic.Int32
	if _, err := app.Subscribe("ctl.>", func(*nats.Msg) { got.Add(1) }); err != nil {
		t.Fatal(err)
	}
	_ = app.Flush()
	_ = ctl.Publish("ctl.host.1.start", []byte("x"))
	_ = ctl.Flush()
	time.Sleep(200 * time.Millisecond)
	if got.Load() != 0 {
		t.Fatal("an APP user saw a CTL subject")
	}

	srv := s.Serve()
	ctx := context.Background()
	if _, err := srv.JS().CreateStream(ctx, natsd.Stream(jetstream.StreamConfig{Name: "EVT_1_def", Subjects: []string{"evt.1.def.>"}})); err != nil {
		t.Fatalf("serve's user cannot use JetStream in APP: %v", err)
	}
	cjs, _ := jetstream.New(ctl)
	if _, err := cjs.Stream(ctx, "EVT_1_def"); err == nil {
		t.Fatal("a CTL user reads APP's stream")
	}
}

// TestScopedUser: a daemon-like user allowed only its own subjects cannot
// subscribe to another's.
func TestScopedUser(t *testing.T) {
	s := StartTestSecure(t)
	errs := make(chan error, 4)
	path, _ := s.CredsFile(Control, "host-1", Permissions{Pub: []string{"ctl.evt.host.1.>"}, Sub: []string{"ctl.host.1.>"}}, 0)
	nc, err := nats.Connect(s.URL, nats.UserCredentials(path), nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) { errs <- err }))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	if _, err := nc.SubscribeSync("ctl.host.2.>"); err != nil {
		t.Fatal(err)
	}
	_ = nc.Flush()
	select {
	case err := <-errs:
		if !strings.Contains(strings.ToLower(err.Error()), "permissions violation") {
			t.Fatalf("error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscribing to another host's subjects was allowed")
	}
}

// TestPushedAccountsTakeEffect: a new account pushed through the resolver
// accepts its users at once; revoking a user and pushing disconnects it;
// an expired user is refused.
func TestPushedAccountsTakeEffect(t *testing.T) {
	s := StartTestSecure(t)
	sys := s.System()

	tenant, err := s.D.AddAccount("T5", Limits{Storage: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Conn("T5", "early", Everything); err == nil {
		t.Fatal("a user of an account not yet pushed connected")
	}
	if err := Push(sys, tenant); err != nil {
		t.Fatal(err)
	}
	path, u := s.CredsFile("T5", "user", Everything, 0)
	closed := make(chan struct{})
	nc, err := nats.Connect(s.URL, nats.UserCredentials(path), nats.MaxReconnects(0), nats.ClosedHandler(func(*nats.Conn) { close(closed) }))
	if err != nil {
		t.Fatalf("a user of a pushed account: %v", err)
	}
	defer nc.Close()

	tenant.Revoke(u.Public)
	if err := s.D.Operator.Sign(tenant); err != nil {
		t.Fatal(err)
	}
	if err := Push(sys, tenant); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a revoked user stayed connected")
	}
	if _, err := nats.Connect(s.URL, nats.UserCredentials(path), nats.MaxReconnects(0)); err == nil {
		t.Fatal("a revoked user reconnected")
	}

	expired, _ := s.CredsFile(App, "brief", Everything, time.Second)
	time.Sleep(2100 * time.Millisecond) // expiry is in whole seconds
	if _, err := nats.Connect(s.URL, nats.UserCredentials(expired), nats.MaxReconnects(0)); err == nil {
		t.Fatal("an expired user connected")
	}
}

// TestSaveLoad: a saved deployment loads with the same keys, and without
// the operator seed it still mints users.
func TestSaveLoad(t *testing.T) {
	d, err := Init("x")
	if err != nil {
		t.Fatal(err)
	}
	k := DirKeys(t.TempDir())
	if err := d.Save(k); err != nil {
		t.Fatal(err)
	}
	l, err := Load(k)
	if err != nil {
		t.Fatal(err)
	}
	if l.Account(App).Public() != d.Account(App).Public() || l.Operator.Key == nil {
		t.Fatal("loaded deployment differs")
	}
	if !strings.Contains(d.ServerConfig("/r"), d.Account(System).Public()) {
		t.Fatal("server config lacks the system account")
	}
	if err := osRemove(string(k), "operator.nk"); err != nil {
		t.Fatal(err)
	}
	l, err = Load(k)
	if err != nil || l.Operator.Key != nil {
		t.Fatalf("offline operator: %v", err)
	}
	if _, err := l.Account(App).NewUser("u", Everything, 0); err != nil {
		t.Fatal(err)
	}
}
