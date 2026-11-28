package https_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/contracttest"
	githttps "github.com/SgtFlame/foundation/backend/features/connectors/git/https"
)

const backend = "/usr/lib/git-core/git-http-backend"

// server serves a bare repository over smart HTTP with git's own
// http-backend, behind a basic-auth check (user "u", token "tok").
type server struct {
	URL  string
	bare string
	t    *testing.T
}

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

func start(t *testing.T) *server {
	t.Helper()
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend is not installed")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "site.git")
	run(t, root, "init", "--bare", "-b", "main", bare)
	run(t, bare, "config", "http.receivepack", "true")
	work := filepath.Join(root, "seed")
	run(t, root, "clone", bare, work)
	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# Site\n"), 0o644)
	run(t, work, "add", ".")
	run(t, work, "commit", "-m", "Initial commit")
	run(t, work, "push", "origin", "HEAD:main")
	h := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=u"}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "tok" {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return &server{URL: ts.URL + "/site.git", bare: bare, t: t}
}

// push commits a file to a branch from outside, as someone else would.
func (s *server) push(branch, path, content string) string {
	t := s.t
	work := t.TempDir()
	run(t, work, "clone", "-q", s.bare, "w")
	w := filepath.Join(work, "w")
	if run(t, w, "ls-remote", "origin", "refs/heads/"+branch) != "" {
		run(t, w, "checkout", "-q", "-B", branch, "origin/"+branch)
	} else {
		run(t, w, "checkout", "-q", "-b", branch)
	}
	_ = os.MkdirAll(filepath.Dir(filepath.Join(w, path)), 0o755)
	_ = os.WriteFile(filepath.Join(w, path), []byte(content), 0o644)
	run(t, w, "add", ".")
	run(t, w, "commit", "-q", "-m", "From outside")
	run(t, w, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return run(t, w, "rev-parse", "HEAD")
}

func ctxFor(contract, op string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: contract, Operation: op,
		Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, TenantID: 1, UserID: 2}}}
}

func open(t *testing.T, s *server, token string) adapter.Session {
	t.Helper()
	settings, _ := json.Marshal(githttps.Settings{URL: s.URL, Username: "u", Token: token})
	sess, err := githttps.Impl(githttps.Options{Poll: 30 * time.Millisecond, AllowHTTP: true}).Open(ctxFor("", ""), adapter.Connection{ID: "c1", Type: githttps.TypeID, Tenant: 1, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestMetadata(t *testing.T) {
	cs := map[string]connectors.Contract{git.RepoRef: git.RepoContract()}
	if ps := connectors.CheckType(githttps.Type(), func(ref string) (connectors.Contract, bool) { c, ok := cs[ref]; return c, ok }); len(ps) > 0 {
		t.Fatal(ps)
	}
}

func TestSuite(t *testing.T) {
	s := start(t)
	sess := open(t, s, "tok")
	contracttest.Run(t, contracttest.SessionCaller(sess, ctxFor), contracttest.Options{
		Push: func(branch string) (string, error) { return s.push(branch, "outside.md", "from outside"), nil },
		Watch: func(t *testing.T, emit func(adapter.Event)) func() {
			wctx, cancel := context.WithCancel(context.Background())
			x := ctxFor(git.RepoRef, "")
			x.Context = wctx
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = sess.(adapter.Watcher).Watch(x, nil, func(e adapter.Event) error { emit(e); return nil })
			}()
			time.Sleep(300 * time.Millisecond) // the first look notes where things are
			return func() { cancel(); <-done }
		},
	})
}

func call(sess adapter.Session, op string, args any) (json.RawMessage, error) {
	raw, _ := json.Marshal(args)
	return sess.Call(ctxFor(git.RepoRef, op), op, raw)
}

func code(err error) string {
	var ce *connectors.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func TestErrors(t *testing.T) {
	s := start(t)
	if _, err := call(open(t, s, "wrong"), "check", map[string]any{}); code(err) != connectors.CodeAuth {
		t.Fatalf("a wrong token: %v, want auth", err)
	}
	if _, err := githttps.New(githttps.Settings{URL: "http://example.com/x.git"}, githttps.Options{}); code(err) != connectors.CodeInvalid {
		t.Fatalf("plain http: %v, want invalid", err)
	}
	sess := open(t, s, "tok")
	if _, err := call(sess, "readFile", map[string]any{"ref": "nope", "path": "README.md"}); code(err) != connectors.CodeNotFound {
		t.Fatalf("a missing ref: %v, want notFound", err)
	}
	// A push from outside between the read and the commit: conflict
	raw, err := call(sess, "listRefs", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var refs struct{ Refs []git.Ref }
	_ = json.Unmarshal(raw, &refs)
	s.push("main", "other.md", "x")
	_, err = call(sess, "commit", map[string]any{"branch": "main", "expectHead": refs.Refs[0].SHA, "message": "x", "changes": []map[string]any{{"path": "a.md", "content": "a"}}})
	if code(err) != connectors.CodeConflict {
		t.Fatalf("a stale head: %v, want conflict", err)
	}
	// A shallow history still commits on top of the head, keeping files
	if _, err := call(sess, "commit", map[string]any{"branch": "main", "message": "Nested", "changes": []map[string]any{{"path": "www/content/code/x/index.md", "content": "# X\n"}}}); err != nil {
		t.Fatal(err)
	}
	log := run(t, s.bare, "log", "--format=%s", "main")
	if !strings.HasPrefix(log, "Nested\nFrom outside\nInitial commit") {
		t.Fatalf("history: %q", log)
	}
	if got := run(t, s.bare, "show", "main:www/content/code/x/index.md"); got != "# X" {
		t.Fatalf("the nested file: %q", got)
	}
	run(t, s.bare, "fsck", "--strict")
}

// A sha ref is found among the branches' recent commits.
func TestShaRef(t *testing.T) {
	s := start(t)
	sha := s.push("main", "b.md", "b")
	s.push("main", "c.md", "c")
	raw, err := call(open(t, s, "tok"), "readFile", map[string]any{"ref": sha[:10], "path": "b.md"})
	if err != nil {
		t.Fatal(err)
	}
	var f git.File
	_ = json.Unmarshal(raw, &f)
	if f.Content != "b" {
		t.Fatalf("%s", raw)
	}
}
