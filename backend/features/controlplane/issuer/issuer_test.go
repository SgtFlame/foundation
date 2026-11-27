package issuer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault/vaulttest"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
)

type env struct {
	s     *natsauth.TestServer
	c     *ctl.Control
	vault *vaulttest.Fake
}

func setup(t *testing.T, processes int, opts ...func(*Issuer)) *env {
	t.Helper()
	s := natsauth.StartTestSecure(t)
	fake := vaulttest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var first *ctl.Control
	for p := 0; p < processes; p++ {
		nc, err := s.Conn(natsauth.Control, fmt.Sprintf("serve%d", p), natsauth.Everything)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nc.Close)
		js, _ := jetstream.New(nc)
		c := &ctl.Control{NC: nc, JS: js}
		if first == nil {
			first = c
			if err := c.Ensure(ctx); err != nil {
				t.Fatal(err)
			}
		}
		v, _ := vault.New(fake.URL, fake.Token)
		i := &Issuer{C: c, D: s.D, Sys: s.System(), Vault: v}
		for _, o := range opts {
			o(i)
		}
		if err := i.Serve(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return &env{s: s, c: first, vault: fake}
}

// place puts the instance's desired state with serve units.
func (e *env) place(t *testing.T, instance string, units ...map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"instance": map[string]any{"name": instance}, "units": units})
	if _, err := e.c.PutDesired(context.Background(), ctl.Target{Kind: ctl.KindInstance, ID: instance}, raw); err != nil {
		t.Fatal(err)
	}
}

// agent is a connection with an instance's own daemon permissions.
func (e *env) agent(t *testing.T, instance string) *ctl.Control {
	t.Helper()
	nc, err := e.s.Conn(natsauth.Control, "axiomd-inst."+instance, ctl.DaemonPermissions(ctl.Target{Kind: ctl.KindInstance, ID: instance}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return &ctl.Control{NC: nc}
}

type unitCreds struct {
	rep  ctl.UnitReply
	seed []byte
}

func ask(t *testing.T, a *ctl.Control, instance, unit string) (unitCreds, error) {
	t.Helper()
	kp, _ := nkeys.CreateUser()
	pub, _ := kp.PublicKey()
	seed, _ := kp.Seed()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) // a refused publish answers nothing
	defer cancel()
	rep, err := a.AskIssuer(ctx, instance, ctl.UnitRequest{Unit: unit, PublicKey: pub})
	return unitCreds{rep: rep, seed: seed}, err
}

func (e *env) connect(u unitCreds) (*nats.Conn, error) {
	return nats.Connect(e.s.URL, nats.UserJWTAndSeed(u.rep.JWT, string(u.seed)), nats.MaxReconnects(0))
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

// TestIssue: a placed unit gets a user of the services account and a
// wrapping token that unwraps once to a SecretID logging in with the
// set's RoleID; another instance's agent, and a unit not placed, get
// nothing.
func TestIssue(t *testing.T) {
	ctx := context.Background()
	e := setup(t, 1)
	e.place(t, "i1", map[string]any{"name": "web", "kind": "serve", "serviceSet": "web"}, map[string]any{"name": "nginx", "kind": "other"})
	a := e.agent(t, "i1")
	u, err := ask(t, a, "i1", "web")
	if err != nil {
		t.Fatal(err)
	}
	if u.rep.Account != natsauth.App || u.rep.ExpiresAt.Before(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("reply %+v", u.rep)
	}
	nc, err := e.connect(u)
	if err != nil {
		t.Fatalf("the unit's user cannot connect: %v", err)
	}
	nc.Close()
	v, _ := vault.New(e.vault.URL, "")
	sid, err := v.UnwrapSecretID(ctx, u.rep.WrappedSecretID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.UnwrapSecretID(ctx, u.rep.WrappedSecretID); err == nil {
		t.Fatal("unwrapped twice")
	}
	if _, err := v.LoginAppRole(ctx, "approle", u.rep.RoleID, sid); err != nil {
		t.Fatalf("login: %v", err)
	}
	if r := e.vault.Roles()["unit-web"]; r.RoleID != u.rep.RoleID || len(r.Policies) != 1 || e.vault.Policy("unit-web") == "" {
		t.Fatalf("role %+v", r)
	}

	if _, err := ask(t, a, "i1", "nginx"); err == nil {
		t.Fatal("issued for a unit that is not serve")
	}
	if _, err := ask(t, a, "i1", "other"); err == nil {
		t.Fatal("issued for a unit not placed")
	}
	if _, err := ask(t, a, "i2", "web"); err == nil {
		t.Fatal("an agent asked as another instance")
	}
	e.place(t, "i2", map[string]any{"name": "web2", "kind": "serve"})
	if _, err := ask(t, a, "i2", "web2"); err == nil {
		t.Fatal("an agent got another instance's unit")
	}
}

// TestRevokeOnRemoval: removing the unit makes NATS refuse its user and
// Vault its SecretID.
func TestRevokeOnRemoval(t *testing.T) {
	ctx := context.Background()
	e := setup(t, 1)
	e.place(t, "i1", map[string]any{"name": "web", "kind": "serve"})
	a := e.agent(t, "i1")
	u, err := ask(t, a, "i1", "web")
	if err != nil {
		t.Fatal(err)
	}
	if nc, err := e.connect(u); err != nil {
		t.Fatal(err)
	} else {
		nc.Close()
	}
	e.place(t, "i1") // the unit is removed
	until(t, "the user is refused", func() bool {
		nc, err := e.connect(u)
		if err == nil {
			nc.Close()
		}
		return err != nil
	})
	v, _ := vault.New(e.vault.URL, "")
	sid, err := v.UnwrapSecretID(ctx, u.rep.WrappedSecretID)
	if err == nil {
		if _, err := v.LoginAppRole(ctx, "approle", u.rep.RoleID, sid); err == nil {
			t.Fatal("the SecretID still logs in")
		}
	}
	// A unit placed again gets new credentials that work
	e.place(t, "i1", map[string]any{"name": "web", "kind": "serve"})
	u2, err := ask(t, a, "i1", "web")
	if err != nil {
		t.Fatal(err)
	}
	if nc, err := e.connect(u2); err != nil {
		t.Fatalf("new credentials after a revocation: %v", err)
	} else {
		nc.Close()
	}
}

// TestTwoProcesses: two issuers answer concurrently without conflict, and
// an explicit revocation reaches every issue.
func TestTwoProcesses(t *testing.T) {
	e := setup(t, 2)
	e.place(t, "i1", map[string]any{"name": "web", "kind": "serve"})
	a := e.agent(t, "i1")
	var wg sync.WaitGroup
	creds := make([]unitCreds, 10)
	errs := make([]error, 10)
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			creds[n], errs[n] = ask(t, a, "i1", "web")
		}(n)
	}
	wg.Wait()
	for n, err := range errs {
		if err != nil {
			t.Fatalf("ask %d: %v", n, err)
		}
	}
	nc, _ := e.s.Conn(natsauth.Control, "cp", natsauth.Everything)
	defer nc.Close()
	m, err := nc.Request(RevokeSubject, []byte(`{"instance":"i1","unit":"web"}`), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var rep struct{ Revoked int }
	_ = json.Unmarshal(m.Data, &rep)
	if rep.Revoked != 10 {
		t.Fatalf("revoked %s", m.Data)
	}
	for n, u := range creds {
		until(t, fmt.Sprintf("issue %d is refused", n), func() bool {
			c, err := e.connect(u)
			if err == nil {
				c.Close()
			}
			return err != nil
		})
	}
}

// TestSetOf (forge 21h): with SetOf, any set serve takes is issued with
// the policy it computes, a set without config reading no configuration
// secret, and a set it refuses gets nothing.
func TestSetOf(t *testing.T) {
	e := setup(t, 1, func(i *Issuer) {
		i.SetOf = func(set string) (Set, bool) {
			switch set {
			case "web":
				return Set{Permissions: natsauth.Everything, Policy: ServerPolicy}, true
			case "data,runner":
				return Set{Permissions: natsauth.Everything, Policy: ServerPolicy + ConfigPolicy}, true
			}
			return Set{}, false
		}
	})
	e.place(t, "i1", map[string]any{"name": "web", "kind": "serve", "serviceSet": "web"},
		map[string]any{"name": "data", "kind": "serve", "serviceSet": "data,runner"},
		map[string]any{"name": "odd", "kind": "serve", "serviceSet": "nope"})
	a := e.agent(t, "i1")
	if _, err := ask(t, a, "i1", "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(t, a, "i1", "data"); err != nil {
		t.Fatal(err)
	}
	if p := e.vault.Policy("unit-web"); !strings.Contains(p, "server/*") || strings.Contains(p, "config/*") {
		t.Fatalf("web's policy %q", p)
	}
	if p := e.vault.Policy(RoleName("data,runner")); RoleName("data,runner") != "unit-data_runner" || !strings.Contains(p, "config/*") {
		t.Fatalf("data's policy %q", p)
	}
	if _, err := ask(t, a, "i1", "odd"); err == nil {
		t.Fatal("issued for a set SetOf refuses")
	}
}
