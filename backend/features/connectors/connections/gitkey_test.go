package connections_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	gitssh "github.com/SgtFlame/foundation/backend/features/connectors/git/ssh"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/ssh/sshtest"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

const passphrase = "a passphrase never stored 7f3a"

func gitRun(t *testing.T, dir string, a ...string) {
	t.Helper()
	cmd := exec.Command("git", a...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=O", "GIT_AUTHOR_EMAIL=o@example.com", "GIT_COMMITTER_NAME=O", "GIT_COMMITTER_EMAIL=o@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", a, err, out)
	}
}

// secretCall runs the connection.secretCall live call as the browser's
// form would, giving its one result event.
func (f *fixture) secretCall(as features.ServiceArgs, conn, contract, op string, in any) map[string]any {
	f.t.Helper()
	var call live.Call
	for _, c := range live.Calls.All(f.reg) {
		if c.ID == "connection.secretCall" {
			call = c
		}
	}
	raw, _ := json.Marshal(map[string]any{"connection": conn, "contract": contract, "operation": op, "args": in})
	var got map[string]any
	if fail := call.Run(live.CallRun{ServiceArgs: as, Args: raw, Emit: func(event string, data any) {
		b, _ := json.Marshal(data)
		_ = json.Unmarshal(b, &got)
	}}); fail != nil {
		f.t.Fatalf("connection.secretCall: %s", fail.Message)
	}
	return got
}

// TestKeyNeverStored: a git.ssh connection's key is unlocked through
// connection.secretCall, its calls work while unlocked, and the
// passphrase is in no event, KV value, stream message, log record, query
// or secret, and on the bus only in the one call to the adapter (forge
// 24f). Nothing else may send the unlock.
func TestKeyNeverStored(t *testing.T) {
	if _, err := exec.LookPath("git-upload-pack"); err != nil {
		t.Skip("git is not installed")
	}
	logs := &syncBuffer{}
	if err := logging.Setup(logs, "json", slog.LevelDebug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logging.Setup(os.Stderr, "text", slog.LevelInfo) })
	f := setup(t)
	git.Contribute(f.reg)
	gitssh.Contribute(f.reg)
	var traffic, unlocks syncBuffer
	tap, err := f.srv.Connect("tap")
	if err != nil {
		t.Fatal(err)
	}
	defer tap.Close()
	if _, err := tap.Subscribe(">", func(m *nats.Msg) {
		switch {
		case strings.HasSuffix(m.Subject, ".unlock"):
			fmt.Fprintf(&unlocks, "%s\n", m.Subject)
		case strings.Contains(string(m.Data), passphrase):
			fmt.Fprintf(&traffic, "%s %s\n", m.Subject, m.Data)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_ = tap.Flush()

	root := t.TempDir()
	bare := filepath.Join(root, "site.git")
	gitRun(t, root, "init", "--bare", "-b", "main", bare)
	work := filepath.Join(root, "seed")
	gitRun(t, root, "clone", bare, work)
	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# Site\n"), 0o644)
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-m", "Initial commit")
	gitRun(t, work, "push", "origin", "HEAD:main")
	priv, pub := sshtest.Key(t, passphrase)
	srv := sshtest.Start(t, root, pub)

	f.serve(adapter.Options{Name: "git", Types: []connectors.Type{gitssh.Type()}, Contracts: git.Contracts(), Impls: []adapter.Impl{gitssh.Impl(gitssh.Options{Keys: gitssh.NewKeyring()})}})
	f.online(gitssh.TypeID)
	c := f.create(map[string]any{"type": gitssh.TypeID, "name": "Site", "settings": map[string]any{"url": srv.URL("site.git"), "knownHosts": srv.KnownHosts},
		"secrets": map[string]any{"privateKey": priv}})
	repo := func(op string) *connectors.Error {
		_, e := f.deps.Dispatcher.Call(args(), connectors.Call{Contract: git.RepoRef, Operation: op, Connection: c.ID, Args: json.RawMessage(`{}`)})
		return e
	}
	// Locked: auth, detail locked, and the connection stays ok
	if e := repo("check"); e == nil || e.Detail != connectors.DetailLocked {
		t.Fatalf("a locked call: %v", e)
	}
	time.Sleep(100 * time.Millisecond)
	if c2 := f.connection(c.ID, 0); c2.Status == connections.StatusError {
		t.Fatalf("a locked key failed the connection: %+v", c2)
	}
	// Nothing but connection.secretCall sends unlock
	raw, _ := json.Marshal(map[string]any{"passphrase": passphrase})
	if _, e := f.deps.Dispatcher.Call(args(), connectors.Call{Contract: git.KeyRef, Operation: "unlock", Connection: c.ID, Args: raw}); e == nil || e.Code != connectors.CodeInvalid {
		t.Fatalf("a plain call sent unlock: %v", e)
	}
	// A wrong passphrase, then the right one
	if got := f.secretCall(args(), c.ID, git.KeyRef, "unlock", map[string]any{"passphrase": "wrong one"}); got["error"] == nil {
		t.Fatalf("a wrong passphrase: %v", got)
	}
	got := f.secretCall(args(), c.ID, git.KeyRef, "unlock", map[string]any{"passphrase": passphrase, "minutes": 5})
	if res, _ := got["result"].(map[string]any); res["unlocked"] != true {
		t.Fatalf("unlock: %v", got)
	}
	if e := repo("check"); e != nil {
		t.Fatalf("an unlocked call: %v", e)
	}
	// Only a secret operation goes through it
	if got := f.secretCall(args(), c.ID, git.RepoRef, "check", map[string]any{}); got["error"] == nil {
		t.Fatalf("secretCall sent check: %v", got)
	}
	list := f.query(args(), "connections.describe", map[string]any{"id": c.ID})

	// Look everywhere something is kept
	time.Sleep(200 * time.Millisecond)
	var kept strings.Builder
	ctx := context.Background()
	js := f.srv.JS()
	for name := range js.StreamNames(ctx).Name() {
		s, err := js.Stream(ctx, name)
		if err != nil {
			continue
		}
		info, _ := s.Info(ctx)
		for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
			if m, err := s.GetMsg(ctx, seq); err == nil {
				fmt.Fprintf(&kept, "%s %s %v\n", name, m.Data, m.Header)
			}
		}
	}
	f.secrets.mu.Lock()
	for _, v := range f.secrets.values {
		kept.WriteString(v)
	}
	f.secrets.mu.Unlock()
	if !strings.Contains(unlocks.String(), "cx.call.1.git_ssh."+c.ID+".unlock") {
		t.Fatalf("the tap saw no unlock: %s", unlocks.String())
	}
	if !strings.Contains(kept.String(), "OPENSSH PRIVATE KEY") {
		t.Fatal("the scan found not even the private key in its secret")
	}
	for what, text := range map[string]string{"a stream, KV bucket or secret": kept.String(), "a log record": logs.String(), "a query": list.json(), "the bus (but the unlock)": traffic.String()} {
		if strings.Contains(text, passphrase) {
			t.Errorf("the passphrase is in %s", what)
		}
	}
	// Lock forgets it
	if got := f.secretCall(args(), c.ID, git.KeyRef, "unlock", map[string]any{"passphrase": passphrase}); got["result"] == nil {
		t.Fatal(got)
	}
	if _, e := f.deps.Dispatcher.Call(args(), connectors.Call{Contract: git.KeyRef, Operation: "lock", Connection: c.ID, Args: json.RawMessage(`{}`)}); e != nil {
		t.Fatal(e)
	}
	if e := repo("check"); e == nil || e.Detail != connectors.DetailLocked {
		t.Fatalf("after lock: %v", e)
	}
}
