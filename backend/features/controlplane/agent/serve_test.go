package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/issuer"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault/vaulttest"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// standIn is what serve --instance does with its credentials until 21g:
// connect with nats.creds, unwrap the SecretID and log in to Vault.
func standIn(t *testing.T, dir, url, vaultURL string) error {
	t.Helper()
	nc, err := nats.Connect(url, nats.UserCredentials(filepath.Join(dir, "nats.creds")), nats.MaxReconnects(0))
	if err != nil {
		return err
	}
	nc.Close()
	role, _ := os.ReadFile(filepath.Join(dir, "vault-role-id"))
	wrapped, _ := os.ReadFile(filepath.Join(dir, "vault-wrapped-secret-id"))
	v, _ := vault.New(vaultURL, "")
	sid, err := v.UnwrapSecretID(context.Background(), string(wrapped))
	if err != nil {
		return err
	}
	_, err = v.LoginAppRole(context.Background(), "approle", string(role), sid)
	return err
}

// TestServeUnits: a placed serve unit is run from the template unit; its
// credentials, asked through the local socket before each start, connect
// and log in, are new on every start, and are refused for a unit not
// placed here; a removed unit's user is refused.
func TestServeUnits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := natsauth.StartTestSecure(t)
	fake := vaulttest.New(t)
	cp, _ := s.Conn(natsauth.Control, "serve", natsauth.Everything)
	defer cp.Close()
	js, _ := jetstream.New(cp)
	control := &ctl.Control{NC: cp, JS: js}
	if err := control.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(fake.URL, fake.Token)
	if err := (&issuer.Issuer{C: control, D: s.D, Sys: s.System(), Vault: v}).Serve(ctx); err != nil {
		t.Fatal(err)
	}
	inst := ctl.Target{Kind: ctl.KindInstance, ID: "i1"}
	own, err := s.Conn(natsauth.Control, "axiomd-inst.i1", ctl.DaemonPermissions(inst))
	if err != nil {
		t.Fatal(err)
	}
	defer own.Close()
	ownJS, _ := jetstream.New(own)
	f := newFake(t)
	agentCtl := &ctl.Control{NC: own, JS: ownJS}
	f.a.Control, f.a.Instance, f.a.NatsURLs = func() *ctl.Control { return agentCtl }, "i1", []string{s.URL}
	sock := filepath.Join(t.TempDir(), "agent.sock")
	if err := f.a.ServeLocal(ctx, sock); err != nil {
		t.Fatal(err)
	}

	units := []map[string]any{{"name": "web", "kind": "serve", "serviceSet": "web"}, {"name": "jobs", "kind": "serve", "serviceSet": "runner"}}
	doc := map[string]any{"instance": map[string]any{"name": "i1"}, "units": units}
	raw, _ := json.Marshal(doc)
	if _, err := control.PutDesired(ctx, inst, raw); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Reconcile(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if f.count("systemctl enable --now axiom-serve@web") != 1 || f.count("systemctl enable --now axiom-serve@jobs") != 1 {
		t.Fatalf("calls %v", f.calls())
	}
	tmpl, _ := os.ReadFile(filepath.Join(f.a.Root, "etc/systemd/system/axiom-serve@.service"))
	if !strings.Contains(string(tmpl), "LoadCredential=nats.creds:/run/axiomd/units/%i/nats.creds") || !strings.Contains(string(tmpl), "ExecStartPost=+/bin/rm -rf /run/axiomd/units/%i") {
		t.Fatalf("template unit:\n%s", tmpl)
	}
	cfg, _ := os.ReadFile(filepath.Join(f.a.Root, "etc/axiom/units/web.json"))
	if !strings.Contains(string(cfg), `"serviceSet": "web"`) {
		t.Fatalf("instance config %s", cfg)
	}

	// systemd starts axiom-serve-creds@web, which asks the agent
	if err := AskLocal(sock, "web"); err != nil {
		t.Fatal(err)
	}
	dir := f.a.runDir("web")
	if st, err := os.Stat(filepath.Join(dir, "nats.creds")); err != nil || st.Mode().Perm() != 0o400 {
		t.Fatalf("creds file %v %v", st, err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "vault-wrapped-secret-id"))
	if err := standIn(t, dir, s.URL, fake.URL); err != nil {
		t.Fatalf("the unit cannot use its credentials: %v", err)
	}
	// A restart asks again: a new SecretID
	if err := AskLocal(sock, "web"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "vault-wrapped-secret-id"))
	if string(first) == string(second) {
		t.Fatal("a restart reused the SecretID")
	}
	if err := standIn(t, dir, s.URL, fake.URL); err != nil {
		t.Fatalf("after a restart: %v", err)
	}
	keep, _ := os.ReadFile(filepath.Join(dir, "nats.creds"))
	if err := AskLocal(sock, "other"); err == nil {
		t.Fatal("got credentials for a unit not placed here")
	}

	// Renewal: a user near its expiry restarts its unit, one per set
	m := f.a.issuedUnits()
	m["web"] = issued{ExpiresAt: time.Now().Add(time.Hour)}
	raw2, _ := json.Marshal(m)
	_ = os.WriteFile(f.a.issuedFile(), raw2, 0o600)
	if err := f.a.Reconcile(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if f.count("systemctl restart axiom-serve@web") != 1 {
		t.Fatalf("no renewal: %v", f.calls())
	}

	// The unit removed: stopped, forgotten, its user refused
	doc["units"] = units[1:]
	raw, _ = json.Marshal(doc)
	if _, err := control.PutDesired(ctx, inst, raw); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Reconcile(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.a.issuedUnits()["web"]; ok {
		t.Fatal("the removed unit is still kept")
	}
	credsFile := filepath.Join(t.TempDir(), "web.creds")
	_ = os.WriteFile(credsFile, keep, 0o600)
	deadline := time.Now().Add(10 * time.Second)
	for {
		nc, err := nats.Connect(s.URL, nats.UserCredentials(credsFile), nats.MaxReconnects(0))
		if err != nil {
			break
		}
		nc.Close()
		if time.Now().After(deadline) {
			t.Fatal("the removed unit's user still connects")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
