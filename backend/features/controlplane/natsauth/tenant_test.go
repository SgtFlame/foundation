package natsauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestTenantAccountsKeepStreamsApart: with per-tenant accounts, the live
// core makes a tenant's event stream in that tenant's account, where
// neither the application account nor another tenant's sees it.
func TestTenantAccountsKeepStreamsApart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := StartTestSecure(t)
	sys := s.System()
	for _, id := range []uint64{10, 11} {
		a, err := s.D.AddTenant(id, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		if err := Push(sys, a); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.D.AddTenant(10, Limits{}); err == nil {
		t.Fatal("a second account for tenant 10 was made")
	}
	srv := s.Serve()
	acc := natsd.NewPerTenant(natsd.Options{URL: s.URL}, Minter(s.D, nil, "serve"))
	t.Cleanup(acc.Close)
	p := project.New(srv.JS(), extensions.NewRegistry())
	p.SetAccounts(acc)

	if _, err := p.Stream(ctx, 10, live.AreaDefinitions); err != nil {
		t.Fatal(err)
	}
	js10, err := p.JetStreamFor(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	subject := fmt.Sprintf("evt.10.def.graph.g1.made")
	if _, err := js10.Publish(ctx, subject, []byte("{}")); err != nil {
		t.Fatalf("tenant 10 cannot commit to its own stream: %v", err)
	}
	name := live.StreamName(10, live.AreaDefinitions)
	if _, err := srv.JS().Stream(ctx, name); !errors.Is(err, jetstream.ErrStreamNotFound) {
		t.Fatalf("the application account sees %s: %v", name, err)
	}
	js11, err := p.JetStreamFor(ctx, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js11.Stream(ctx, name); !errors.Is(err, jetstream.ErrStreamNotFound) {
		t.Fatalf("tenant 11 sees %s: %v", name, err)
	}
	// Tenant 11 publishing on tenant 10's subjects reaches no stream
	if _, err := js11.Publish(ctx, subject, []byte("{}")); !errors.Is(err, nats.ErrNoResponders) && !errors.Is(err, jetstream.ErrNoStreamResponse) {
		t.Fatalf("tenant 11 committed to tenant 10's subjects: %v", err)
	}
	if _, err := acc.For(ctx, 12); err == nil {
		t.Fatal("tenant 12, which has no account, was reached")
	}
}

// TestMinterFindsNewAccounts: an account saved after serve loaded its
// deployment is found in the keys when first asked for.
func TestMinterFindsNewAccounts(t *testing.T) {
	keys := DirKeys(t.TempDir())
	d, err := Init("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Save(keys); err != nil {
		t.Fatal(err)
	}
	mint := Minter(d, keys, "serve")
	if _, err := mint(context.Background(), 5); err == nil {
		t.Fatal("tenant 5 has no account yet")
	}
	other, err := Load(keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.AddTenant(5, Limits{}); err != nil {
		t.Fatal(err)
	}
	if err := other.SaveAccount(keys, TenantAccount(5)); err != nil {
		t.Fatal(err)
	}
	creds, err := mint(context.Background(), 5)
	if err != nil || len(creds) == 0 {
		t.Fatalf("tenant 5's user: %v", err)
	}
	if id, ok := TenantOf(TenantAccount(5)); !ok || id != 5 {
		t.Fatalf("TenantOf(T5) = %d, %v", id, ok)
	}
	if _, ok := TenantOf(App); ok {
		t.Fatal("APP is not a tenant account")
	}
}

// TestApplyTenant: a tenant account made and limited from its desired
// limits takes effect on a running server with no restart: a stream past
// the limit is refused, and raising the limit lets it through.
func TestApplyTenant(t *testing.T) {
	ctx := context.Background()
	s := StartTestSecure(t)
	keys := DirKeys(t.TempDir())
	if err := s.D.Save(keys); err != nil {
		t.Fatal(err)
	}
	changed, err := ApplyTenant(keys, s.System(), 7, Limits{Streams: 1})
	if err != nil || !changed {
		t.Fatalf("apply: %v %v", changed, err)
	}
	if again, err := ApplyTenant(keys, s.System(), 7, Limits{Streams: 1}); err != nil || again {
		t.Fatalf("a second apply changed it: %v %v", again, err)
	}
	d, _ := Load(keys)
	u, err := d.Account(TenantAccount(7)).NewUser("t7", Everything, 0)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "t7.creds")
	_ = os.WriteFile(path, u.Creds, 0o600)
	nc, err := nats.Connect(s.URL, nats.UserCredentials(path), nats.MaxReconnects(0))
	if err != nil {
		t.Fatalf("the tenant's user cannot connect: %v", err)
	}
	defer nc.Close()
	js, _ := jetstream.New(nc)
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "EVT_7_a", Subjects: []string{"a.>"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "EVT_7_b", Subjects: []string{"b.>"}}); err == nil {
		t.Fatal("a second stream past the limit was made")
	}
	if _, err := ApplyTenant(keys, s.System(), 7, Limits{Streams: 2}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "EVT_7_b", Subjects: []string{"b.>"}})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the raised limit did not take effect: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
