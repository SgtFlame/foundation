package natsauth

import (
	"context"
	"errors"
	"fmt"
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
