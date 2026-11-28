package github_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/contracttest"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/github"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/github/githubtest"
)

func open(t *testing.T, token string) (*githubtest.Server, adapter.Session, func(contract, op string) adapter.Context) {
	t.Helper()
	fake := githubtest.Start("acme", "site", "tok")
	t.Cleanup(fake.Close)
	settings, _ := json.Marshal(github.Settings{Owner: "acme", Repo: "site", Token: token, BaseURL: fake.URL})
	ctxFor := func(contract, op string) adapter.Context {
		return adapter.Context{Context: context.Background(), Contract: contract, Operation: op,
			Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, TenantID: 1, UserID: 2}}}
	}
	sess, err := github.Impl(github.Options{Poll: 20 * time.Millisecond}).Open(ctxFor("", ""), adapter.Connection{ID: "c1", Type: github.TypeID, Tenant: 1, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	return fake, sess, ctxFor
}

func TestMetadata(t *testing.T) {
	cs := map[string]connectors.Contract{git.RepoRef: git.RepoContract(), git.ReviewRef: git.ReviewContract()}
	if ps := connectors.CheckType(github.Type(), func(ref string) (connectors.Contract, bool) { c, ok := cs[ref]; return c, ok }); len(ps) > 0 {
		t.Fatal(ps)
	}
}

func TestSuite(t *testing.T) {
	fake, sess, ctx := open(t, "tok")
	contracttest.Run(t, contracttest.SessionCaller(sess, ctx), contracttest.Options{
		Review: true,
		Push:   func(branch string) (string, error) { return fake.Push(branch, "outside.md", "from outside"), nil },
		Watch: func(t *testing.T, emit func(adapter.Event)) func() {
			wctx, cancel := context.WithCancel(context.Background())
			x := ctx(git.RepoRef, "")
			x.Context = wctx
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = sess.(adapter.Watcher).Watch(x, nil, func(e adapter.Event) error { emit(e); return nil })
			}()
			time.Sleep(100 * time.Millisecond) // the first look notes where things are
			return func() { cancel(); <-done }
		},
	})
}

func call(sess adapter.Session, ctx func(string, string) adapter.Context, contract, op string, args any) (json.RawMessage, error) {
	raw, _ := json.Marshal(args)
	return sess.Call(ctx(contract, op), op, raw)
}

func code(err error) *connectors.Error {
	var ce *connectors.Error
	if errors.As(err, &ce) {
		return ce
	}
	return &connectors.Error{}
}

func TestErrors(t *testing.T) {
	_, bad, bctx := open(t, "wrong")
	if _, err := call(bad, bctx, git.RepoRef, "check", map[string]any{}); code(err).Code != connectors.CodeAuth {
		t.Fatalf("a wrong token: %v, want auth", err)
	}
	fake, sess, ctx := open(t, "tok")
	fake.Fail(http.MethodGet, "/git/ref/", http.StatusForbidden, 1)
	_, err := call(sess, ctx, git.RepoRef, "commit", map[string]any{"branch": "main", "message": "x", "changes": []map[string]any{{"path": "a.md", "content": "a"}}})
	if ce := code(err); ce.Code != connectors.CodeRateLimited || !ce.Retry || ce.RetryAfterMs != 7000 {
		t.Fatalf("a rate limit: %+v, want rateLimited after 7s", ce)
	}
	fake.Protect("main")
	if _, err := call(sess, ctx, git.RepoRef, "commit", map[string]any{"branch": "main", "message": "x", "changes": []map[string]any{{"path": "a.md", "content": "a"}}}); code(err).Code != connectors.CodeInvalid {
		t.Fatalf("a protected branch: %v, want invalid", err)
	}
	fake.Fail(http.MethodGet, "", http.StatusBadGateway, 1)
	if _, err := call(sess, ctx, git.RepoRef, "check", map[string]any{}); code(err).Code != connectors.CodeUnavailable || !code(err).Retry {
		t.Fatalf("a 502: %v, want unavailable, retryable", err)
	}
}

// A commit is one Git Data API batch, without cloning: blobs only for
// binary content, one tree, one commit, the ref moved with force false;
func TestCommitWithoutCloning(t *testing.T) {
	fake, sess, ctx := open(t, "tok")
	raw, err := call(sess, ctx, git.RepoRef, "commit", map[string]any{"branch": "main", "message": "Two files", "changes": []map[string]any{
		{"path": "www/content/code/x/index.md", "content": "# X\n"}, {"path": "www/content/code/x/pic.png", "content": "iVBORw0KGgo=", "encoding": "base64"}}})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]int{"POST /git/blobs": 1, "POST /git/trees": 1, "POST /git/commits": 1, "PATCH /git/refs/heads/main": 1} {
		if fake.Requests[k] != want {
			t.Errorf("%s sent %d times, want %d (%v)", k, fake.Requests[k], want, fake.Requests)
		}
	}
	var res git.CommitResult
	_ = json.Unmarshal(raw, &res)
	if res.SHA == "" || res.Before == "" {
		t.Fatalf("%s", raw)
	}
	// Large files come through the blob endpoint
	big := bytes.Repeat([]byte("a"), 1<<20+10)
	fake.Push("main", "big.txt", string(big))
	if _, err := call(sess, ctx, git.RepoRef, "readFile", map[string]any{"ref": "main", "path": "big.txt"}); err == nil || !strings.Contains(err.Error(), "file store") {
		t.Fatalf("a file over 1 MB without a file store: %v", err)
	}
	if fake.Requests["GET /git/blobs/"+gitBlob(big)] != 1 {
		t.Fatalf("the blob was not read: %v", fake.Requests)
	}
}

func gitBlob(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// TestRealRepository runs the suite against a real GitHub repository
// (question 3 of the plan): GIT_GITHUB_REPO=owner/repo and
// GIT_GITHUB_TOKEN, a fine-grained token with Contents and Pull requests
// read and write on it. Its branches start suite/ and end with the run's
// time; delete them after. Skipped without both.
func TestRealRepository(t *testing.T) {
	repo, token := os.Getenv("GIT_GITHUB_REPO"), os.Getenv("GIT_GITHUB_TOKEN")
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || token == "" {
		t.Skip("set GIT_GITHUB_REPO=owner/repo and GIT_GITHUB_TOKEN to run against GitHub")
	}
	sess, err := github.New(github.Settings{Owner: owner, Repo: name, Token: token}, github.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := func(contract, op string) adapter.Context {
		return adapter.Context{Context: context.Background(), Contract: contract, Operation: op,
			Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, TenantID: 1, UserID: 2}}}
	}
	contracttest.Run(t, contracttest.SessionCaller(sess, ctx), contracttest.Options{Review: true, Run: "-" + time.Now().UTC().Format("20060102-150405")})
}
