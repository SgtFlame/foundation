package gittest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/contracttest"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/gittest"
	"github.com/SgtFlame/foundation/backend/features/records"
)

const tenant = 7

func open(t *testing.T, s *gittest.Server, conn string, p features.Principal) (adapter.Session, func(contract, op string) adapter.Context) {
	t.Helper()
	ctxFor := func(contract, op string) adapter.Context {
		return adapter.Context{Context: context.Background(), Args: features.ServiceArgs{Ctx: context.Background(), Principal: p}, Contract: contract, Operation: op, Connection: conn}
	}
	sess, err := s.Impl().Open(ctxFor("", ""), adapter.Connection{ID: conn, Type: gittest.TypeID, Tenant: tenant})
	if err != nil {
		t.Fatal(err)
	}
	return sess, ctxFor
}

func user(id uint64) features.Principal {
	return features.Principal{Kind: features.PrincipalUser, TenantID: tenant, UserID: id}
}

func TestMetadata(t *testing.T) {
	contracts := map[string]connectors.Contract{}
	for _, c := range append(git.Contracts(), gittest.ControlContract()) {
		if ps := connectors.CheckContract(c); len(ps) > 0 {
			t.Errorf("%s: %v", c.Ref(), ps)
		}
		contracts[c.Ref()] = c
	}
	if ps := connectors.CheckType(gittest.Type(), func(ref string) (connectors.Contract, bool) { c, ok := contracts[ref]; return c, ok }); len(ps) > 0 {
		t.Errorf("%s: %v", gittest.TypeID, ps)
	}
}

func TestSuite(t *testing.T) {
	s := gittest.New(nil)
	sess, ctx := open(t, s, "site", user(2))
	call := contracttest.SessionCaller(sess, ctx)
	contracttest.Run(t, call, contracttest.Options{
		Review: true,
		Push: func(branch string) (string, error) {
			raw, err := call(gittest.ControlRef, "push", map[string]any{"branch": branch})
			var r struct{ SHA string }
			_ = json.Unmarshal(raw, &r)
			return r.SHA, err
		},
		Watch: func(t *testing.T, emit func(adapter.Event)) func() {
			wctx, cancel := context.WithCancel(context.Background())
			x := ctx(git.RepoRef, "")
			x.Context = wctx
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = sess.(adapter.Watcher).Watch(x, nil, func(e adapter.Event) error { emit(e); return nil })
			}()
			return func() { cancel(); <-done }
		},
	})
}

func call(t *testing.T, sess adapter.Session, ctx func(string, string) adapter.Context, contract, op string, args any) (json.RawMessage, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return sess.Call(ctx(contract, op), op, raw)
}

func code(err error) string {
	var ce *connectors.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func TestControl(t *testing.T) {
	s := gittest.New(nil)
	sess, ctx := open(t, s, "ctl", user(2))
	commit := map[string]any{"branch": "main", "message": "x", "changes": []map[string]any{{"path": "a.md", "content": "a"}}}

	// Faults fail the next calls, then stop
	if _, err := call(t, sess, ctx, gittest.ControlRef, "fail", map[string]any{"operation": "commit", "times": 2, "kind": "rateLimited"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := call(t, sess, ctx, git.RepoRef, "commit", commit); code(err) != connectors.CodeRateLimited {
			t.Fatalf("call %d: %v, want rateLimited", i+1, err)
		}
	}
	if _, err := call(t, sess, ctx, git.RepoRef, "commit", commit); err != nil {
		t.Fatalf("after the faults: %v", err)
	}
	if _, err := call(t, sess, ctx, gittest.ControlRef, "fail", map[string]any{"operation": "nope"}); code(err) != connectors.CodeInvalid {
		t.Fatalf("a fault on no operation: %v", err)
	}

	// A protected branch refuses commits
	if _, err := call(t, sess, ctx, gittest.ControlRef, "protect", map[string]any{"branch": "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, sess, ctx, git.RepoRef, "commit", commit); code(err) != connectors.CodeInvalid {
		t.Fatalf("a commit to a protected branch: %v, want invalid", err)
	}

	// A merged pull request moves its base
	if _, err := call(t, sess, ctx, git.RepoRef, "commit", map[string]any{"branch": "docs/post", "message": "Post", "changes": []map[string]any{{"path": "post.md", "content": "p"}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := call(t, sess, ctx, git.ReviewRef, "openPullRequest", map[string]any{"head": "docs/post", "title": "Post"})
	if err != nil {
		t.Fatal(err)
	}
	var pr git.PullRequest
	_ = json.Unmarshal(raw, &pr)
	if pr.Base != "main" {
		t.Fatalf("the base defaults to the default branch: %+v", pr)
	}
	if _, err := call(t, sess, ctx, gittest.ControlRef, "merge", map[string]any{"number": pr.Number}); err != nil {
		t.Fatal(err)
	}
	raw, err = call(t, sess, ctx, git.RepoRef, "readFile", map[string]any{"ref": "main", "path": "post.md"})
	if err != nil || !strings.Contains(string(raw), `"content":"p"`) {
		t.Fatalf("after the merge main has post.md: %s %v", raw, err)
	}
	raw, _ = call(t, sess, ctx, git.ReviewRef, "getPullRequest", map[string]any{"number": pr.Number})
	if !strings.Contains(string(raw), `"state":"merged"`) {
		t.Fatalf("the merged pull request: %s", raw)
	}

	// The log shows branches, commits and pull requests
	raw, err = call(t, sess, ctx, gittest.ControlRef, "log", map[string]any{"limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	var log struct {
		Branches []struct {
			Name      string
			Protected bool
		}
		Commits []struct {
			Message string
			Parents []string
		}
		PullRequests []struct{ State string }
	}
	_ = json.Unmarshal(raw, &log)
	if len(log.Branches) != 2 || !log.Branches[1].Protected || len(log.Commits) != 2 || len(log.Commits[0].Parents) != 2 || log.PullRequests[0].State != "merged" {
		t.Fatalf("log gave %s", raw)
	}

	// Every connection is its own repository
	other, octx := open(t, s, "other", user(2))
	if _, err := call(t, other, octx, git.RepoRef, "readFile", map[string]any{"ref": "main", "path": "post.md"}); code(err) != connectors.CodeNotFound {
		t.Fatalf("another connection sees post.md: %v", err)
	}
}

// files is an in-memory file store.
type files struct {
	mu   sync.Mutex
	data map[string][]byte
	by   map[string]uint64
	next int
}

func (f *files) Put(_ context.Context, _, user uint64, name, contentType string, r io.Reader, max int64) (records.FileRef, error) {
	data, _ := io.ReadAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprint(f.next)
	f.data[id], f.by[id] = data, user
	return records.FileRef{ID: id, Name: name, Size: int64(len(data)), ContentType: contentType}, nil
}

func (f *files) Uploader(_ context.Context, _ uint64, id string) (uint64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.by[id]
	return u, ok, nil
}

func (f *files) Open(_ context.Context, _ uint64, id string) (io.ReadCloser, records.FileRef, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.data[id]
	return io.NopCloser(bytes.NewReader(d)), records.FileRef{ID: id, Size: int64(len(d))}, ok, nil
}

func TestFiles(t *testing.T) {
	store := &files{data: map[string][]byte{}, by: map[string]uint64{}}
	s := gittest.New(store)
	sess, ctx := open(t, s, "big", user(2))
	big := bytes.Repeat([]byte("castle "), git.MaxInline/7+10)
	ref, _ := store.Put(context.Background(), tenant, 2, "big.txt", "text/plain", bytes.NewReader(big), git.MaxFileBytes)

	// Someone else's file does not exist to the caller
	stranger, sctx := open(t, s, "big", user(3))
	commit := map[string]any{"branch": "main", "message": "Big", "changes": []map[string]any{{"path": "big.txt", "file": map[string]any{"id": ref.ID}}}}
	if _, err := call(t, stranger, sctx, git.RepoRef, "commit", commit); code(err) != connectors.CodeNotFound {
		t.Fatalf("committing another's file: %v, want notFound", err)
	}
	if _, err := call(t, sess, ctx, git.RepoRef, "commit", commit); err != nil {
		t.Fatal(err)
	}
	raw, err := call(t, sess, ctx, git.RepoRef, "readFile", map[string]any{"ref": "main", "path": "big.txt"})
	if err != nil {
		t.Fatal(err)
	}
	var f git.File
	_ = json.Unmarshal(raw, &f)
	if f.Content != "" || f.File == nil || f.Size != int64(len(big)) {
		t.Fatalf("a file over 1 MB: %+v", f)
	}
	if !bytes.Equal(store.data[f.File.ID], big) || store.by[f.File.ID] != 2 {
		t.Fatal("the stored file is not the content, kept as the caller")
	}
}
