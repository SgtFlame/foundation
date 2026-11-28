package ssh_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/contracttest"
	gitssh "github.com/SgtFlame/foundation/backend/features/connectors/git/ssh"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/ssh/sshtest"
)

const passphrase = "correct horse battery staple"

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Outside", "GIT_AUTHOR_EMAIL=o@example.com", "GIT_COMMITTER_NAME=Outside", "GIT_COMMITTER_EMAIL=o@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type world struct {
	srv  *sshtest.Server
	bare string
	key  string // the encrypted private key
	keys *gitssh.Keyring
	mu   sync.Mutex
	now  time.Time
	t    *testing.T
}

func start(t *testing.T) *world {
	t.Helper()
	if _, err := exec.LookPath("git-upload-pack"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "site.git")
	run(t, root, "init", "--bare", "-b", "main", bare)
	work := filepath.Join(root, "seed")
	run(t, root, "clone", bare, work)
	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# Site\n"), 0o644)
	run(t, work, "add", ".")
	run(t, work, "commit", "-m", "Initial commit")
	run(t, work, "push", "origin", "HEAD:main")
	priv, pub := sshtest.Key(t, passphrase)
	w := &world{srv: sshtest.Start(t, root, pub), bare: bare, key: priv, keys: gitssh.NewKeyring(), now: time.Now(), t: t}
	w.keys.SetClock(func() time.Time { w.mu.Lock(); defer w.mu.Unlock(); return w.now })
	return w
}

func (w *world) advance(d time.Duration) { w.mu.Lock(); w.now = w.now.Add(d); w.mu.Unlock() }

func (w *world) push(branch, path, content string) string {
	t := w.t
	work := t.TempDir()
	run(t, work, "clone", "-q", w.bare, "w")
	d := filepath.Join(work, "w")
	if run(t, d, "ls-remote", "origin", "refs/heads/"+branch) != "" {
		run(t, d, "checkout", "-q", "-B", branch, "origin/"+branch)
	} else {
		run(t, d, "checkout", "-q", "-b", branch)
	}
	_ = os.MkdirAll(filepath.Dir(filepath.Join(d, path)), 0o755)
	_ = os.WriteFile(filepath.Join(d, path), []byte(content), 0o644)
	run(t, d, "add", ".")
	run(t, d, "commit", "-q", "-m", "From outside")
	run(t, d, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return run(t, d, "rev-parse", "HEAD")
}

func ctxFor(contract, op string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: contract, Operation: op,
		Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, TenantID: 1, UserID: 2}}}
}

func (w *world) open(t *testing.T, st gitssh.Settings) (adapter.Session, error) {
	t.Helper()
	if st.URL == "" {
		st.URL = w.srv.URL("site.git")
	}
	if st.PrivateKey == "" {
		st.PrivateKey = w.key
	}
	if st.KnownHosts == "" {
		st.KnownHosts = w.srv.KnownHosts
	}
	settings, _ := json.Marshal(st)
	return gitssh.Impl(gitssh.Options{Poll: 30 * time.Millisecond, Keys: w.keys}).Open(ctxFor("", ""), adapter.Connection{ID: "c1", Type: gitssh.TypeID, Tenant: 1, Settings: settings})
}

func (w *world) session(t *testing.T) adapter.Session {
	t.Helper()
	s, err := w.open(t, gitssh.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func call(sess adapter.Session, contract, op string, args any) (map[string]any, error) {
	raw, _ := json.Marshal(args)
	res, err := sess.Call(ctxFor(contract, op), op, raw)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	_ = json.Unmarshal(res, &out)
	return out, nil
}

func cerr(err error) *connectors.Error {
	var ce *connectors.Error
	if errors.As(err, &ce) {
		return ce
	}
	return nil
}

func unlock(t *testing.T, sess adapter.Session, minutes int) {
	t.Helper()
	if _, err := call(sess, git.KeyRef, "unlock", map[string]any{"passphrase": passphrase, "minutes": minutes}); err != nil {
		t.Fatal(err)
	}
}

func TestMetadata(t *testing.T) {
	if ps := connectors.CheckContract(git.KeyContract()); len(ps) > 0 {
		t.Fatal(ps)
	}
	cs := map[string]connectors.Contract{git.RepoRef: git.RepoContract(), git.KeyRef: git.KeyContract()}
	if ps := connectors.CheckType(gitssh.Type(), func(ref string) (connectors.Contract, bool) { c, ok := cs[ref]; return c, ok }); len(ps) > 0 {
		t.Fatal(ps)
	}
	if op, _ := git.KeyContract().Operation("unlock"); !op.Secret {
		t.Fatal("unlock is not a secret operation")
	}
	if strings.Contains(string(connectors.OpenAPI(git.KeyContract())), "/unlock") {
		t.Fatal("the OpenAPI document offers unlock")
	}
}

func TestLocking(t *testing.T) {
	w := start(t)
	sess := w.session(t)
	// Locked: every call fails with auth, detail locked
	_, err := call(sess, git.RepoRef, "check", map[string]any{})
	if e := cerr(err); e == nil || e.Code != connectors.CodeAuth || e.Detail != connectors.DetailLocked {
		t.Fatalf("a locked call: %v", err)
	}
	if st, _ := call(sess, git.KeyRef, "keyStatus", map[string]any{}); st["unlocked"] != false || st["encrypted"] != true {
		t.Fatalf("the status: %v", st)
	}
	// A wrong passphrase
	_, err = call(sess, git.KeyRef, "unlock", map[string]any{"passphrase": "wrong"})
	if e := cerr(err); e == nil || e.Code != connectors.CodeInvalid || strings.Contains(e.Message, "wrong") {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	// Unlocked for a minute
	unlock(t, sess, 1)
	if st, _ := call(sess, git.KeyRef, "keyStatus", map[string]any{}); st["unlocked"] != true || st["expiresAt"] == nil {
		t.Fatalf("unlocked: %v", st)
	}
	if res, err := call(sess, git.RepoRef, "check", map[string]any{}); err != nil || res["defaultBranch"] != "main" || res["canWrite"] != true {
		t.Fatalf("check: %v %v", res, err)
	}
	// Another session of the connection (the adapter reopens them) shares it
	other := w.session(t)
	if _, err := call(other, git.RepoRef, "listRefs", map[string]any{}); err != nil {
		t.Fatalf("a reopened session: %v", err)
	}
	// It expires
	w.advance(61 * time.Second)
	if _, err := call(sess, git.RepoRef, "check", map[string]any{}); cerr(err) == nil || cerr(err).Detail != connectors.DetailLocked {
		t.Fatalf("after expiring: %v", err)
	}
	// Lock forgets it at once; minutes are bounded
	unlock(t, sess, 600)
	st, _ := call(sess, git.KeyRef, "keyStatus", map[string]any{})
	until, _ := time.Parse(time.RFC3339, st["expiresAt"].(string))
	if d := until.Sub(w.now); d > time.Duration(gitssh.MaxMinutes)*time.Minute+time.Second {
		t.Fatalf("unlocked for %v", d)
	}
	if _, err := call(sess, git.KeyRef, "lock", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(sess, git.RepoRef, "check", map[string]any{}); cerr(err) == nil {
		t.Fatal("a locked key answered")
	}
	// A new key for the connection is locked though the old one was not
	unlock(t, sess, 15)
	priv2, _ := sshtest.Key(t, "another")
	s2, err := w.open(t, gitssh.Settings{PrivateKey: priv2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call(s2, git.RepoRef, "check", map[string]any{}); cerr(err) == nil || cerr(err).Detail != connectors.DetailLocked {
		t.Fatalf("a new key: %v", err)
	}
}

func TestSuite(t *testing.T) {
	w := start(t)
	sess := w.session(t)
	unlock(t, sess, 60)
	contracttest.Run(t, contracttest.SessionCaller(sess, ctxFor), contracttest.Options{
		Push: func(branch string) (string, error) { return w.push(branch, "outside.md", "from outside"), nil },
		Watch: func(t *testing.T, emit func(adapter.Event)) func() {
			wctx, cancel := context.WithCancel(context.Background())
			x := ctxFor(git.RepoRef, "")
			x.Context = wctx
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = sess.(adapter.Watcher).Watch(x, nil, func(e adapter.Event) error { emit(e); return nil })
			}()
			time.Sleep(300 * time.Millisecond)
			return func() { cancel(); <-done }
		},
	})
	if cmds := w.srv.Commands(); len(cmds) == 0 || !strings.Contains(strings.Join(cmds, " "), "git-receive-pack site.git") {
		t.Fatalf("the server ran %v", cmds)
	}
}

// TestWatchWhileLocked: a locked connection's watch finds nothing, and
// finds the pushes made meanwhile once unlocked.
func TestWatchWhileLocked(t *testing.T) {
	w := start(t)
	sess := w.session(t)
	unlock(t, sess, 15)
	var mu sync.Mutex
	var got []git.Pushed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x := ctxFor(git.RepoRef, "")
	x.Context = ctx
	go func() {
		_ = sess.(adapter.Watcher).Watch(x, nil, func(e adapter.Event) error {
			var p git.Pushed
			_ = json.Unmarshal(e.Payload, &p)
			mu.Lock()
			got = append(got, p)
			mu.Unlock()
			return nil
		})
	}()
	time.Sleep(200 * time.Millisecond)
	_, _ = call(sess, git.KeyRef, "lock", map[string]any{})
	sha := w.push("main", "a.md", "a")
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("a locked watch saw %v", got)
	}
	unlock(t, sess, 15)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n = len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].After != sha {
		t.Fatalf("after unlocking: %v", got)
	}
}

func TestPlainKeyAndHosts(t *testing.T) {
	w := start(t)
	plain, pub := sshtest.Key(t, "")
	srv := sshtest.Start(t, filepath.Dir(w.bare), pub)
	settings := gitssh.Settings{URL: srv.URL("site.git"), PrivateKey: plain, KnownHosts: srv.KnownHosts}
	sess, err := w.open(t, settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call(sess, git.RepoRef, "check", map[string]any{}); err != nil {
		t.Fatalf("a key without a passphrase: %v", err)
	}
	if st, _ := call(sess, git.KeyRef, "keyStatus", map[string]any{}); st["unlocked"] != true || st["encrypted"] != false {
		t.Fatalf("its status: %v", st)
	}
	// A bare key line is trusted for the URL's host
	settings.KnownHosts = strings.SplitN(srv.KnownHosts, " ", 2)[1]
	if sess, err = w.open(t, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := call(sess, git.RepoRef, "check", map[string]any{}); err != nil {
		t.Fatalf("a bare host key: %v", err)
	}
	// Another host's key is refused
	settings.KnownHosts = w.srv.KnownHosts
	if sess, err = w.open(t, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := call(sess, git.RepoRef, "check", map[string]any{}); cerr(err) == nil || cerr(err).Code != connectors.CodeAuth || !strings.Contains(err.Error(), "does not know") {
		t.Fatalf("an unknown host key: %v", err)
	}
	// Refused settings
	for _, bad := range []gitssh.Settings{
		{URL: "https://example.com/x.git", PrivateKey: plain, KnownHosts: srv.KnownHosts},
		{URL: srv.URL("site.git"), PrivateKey: "not a key", KnownHosts: srv.KnownHosts},
		{URL: srv.URL("site.git"), PrivateKey: plain, KnownHosts: "nonsense"},
		{URL: srv.URL("site.git"), PrivateKey: plain, KnownHosts: "|1|abc= " + strings.SplitN(srv.KnownHosts, " ", 2)[1]},
	} {
		if _, err := w.open(t, bad); cerr(err) == nil || cerr(err).Code != connectors.CodeInvalid {
			t.Fatalf("%s / %.20s: %v", bad.URL, bad.KnownHosts, err)
		}
	}
	if _, err := w.open(t, gitssh.Settings{URL: "git@github.com:acme/site.git", PrivateKey: plain, KnownHosts: srv.KnownHosts}); err != nil {
		t.Fatalf("an scp-like URL: %v", err)
	}
	// A key the server does not accept is auth (and marks the connection)
	other, _ := sshtest.Key(t, "")
	settings = gitssh.Settings{URL: srv.URL("site.git"), PrivateKey: other, KnownHosts: srv.KnownHosts}
	sess, _ = w.open(t, settings)
	if _, err := call(sess, git.RepoRef, "check", map[string]any{}); cerr(err) == nil || cerr(err).Code != connectors.CodeAuth || cerr(err).Detail != "" {
		t.Fatalf("a refused key: %v", err)
	}
}
