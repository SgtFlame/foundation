// Package contracttest is the suite every git.repo@1 type passes (forge
// 24a): the Git test server, git.https against a smart-HTTP server in the
// test (24c) and git.github against cassettes (24b).
package contracttest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
)

// Caller calls one operation of the type under test (contract is
// git.repo@1 or git.review@1).
type Caller func(contract, op string, args any) (json.RawMessage, error)

// SessionCaller calls an open session directly, with the context ctx
// makes for each call.
func SessionCaller(s adapter.Session, ctx func(contract, op string) adapter.Context) Caller {
	return func(contract, op string, args any) (json.RawMessage, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		return s.Call(ctx(contract, op), op, raw)
	}
}

// Options say what the suite may use.
type Options struct {
	// Prefix starts every branch the suite makes (suite/), so a real
	// repository can be cleaned up after a recording.
	Prefix string
	// Push commits to a branch from outside the type; nil skips the
	// pushed test. Watch, when Push is set, starts the type's Watch and
	// gives each event; it stops when the test ends.
	Push  func(branch string) (sha string, err error)
	Watch func(t *testing.T, emit func(adapter.Event)) (stop func())
	// Review is set when the type implements git.review@1.
	Review bool
	// Run names the run, so its branches differ from an earlier run's on
	// a real host; empty for a fresh repository.
	Run string
}

func code(err error) string {
	var ce *connectors.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Run runs the suite: check names the default branch; a commit creates a
// branch and readFile gives its content back; readTree lists and pages;
// compare names what changed; a stale expectHead is refused; a retried
// keyed commit makes one commit; changes that change nothing make no
// commit; listRefs finds the branch; an outside push arrives once as
// pushed; and pull requests open once, read and list.
func Run(t *testing.T, call Caller, o Options) {
	t.Helper()
	if o.Prefix == "" {
		o.Prefix = "suite/"
	}
	branch := func(name string) string { return o.Prefix + name + o.Run }
	repo := func(op string, args any, into any) {
		t.Helper()
		raw, err := call(git.RepoRef, op, args)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if into != nil {
			if err := json.Unmarshal(raw, into); err != nil {
				t.Fatalf("%s: %v in %s", op, err, raw)
			}
		}
	}

	var check struct {
		OK            bool
		DefaultBranch string
	}
	repo("check", map[string]any{}, &check)
	if !check.OK || check.DefaultBranch == "" {
		t.Fatalf("check gave %+v", check)
	}
	main := check.DefaultBranch
	var start git.Tree
	repo("readTree", map[string]any{"ref": main}, &start)
	if start.SHA == "" {
		t.Fatalf("readTree %s gave no commit", main)
	}

	// A commit creates the branch from the default branch
	b := branch("one")
	body := "---\ntitle: Castles\n---\n\nHello, castles.\n"
	var c1 git.CommitResult
	repo("commit", map[string]any{"branch": b, "message": "Add a post", "changes": []map[string]any{
		{"path": "posts/castles/index.md", "content": body},
		{"path": "posts/castles/notes.txt", "content": "notes"},
		{"path": "posts/bin.dat", "content": "AAEC/w==", "encoding": "base64"},
	}}, &c1)
	if c1.SHA == "" || !c1.Created || c1.Branch != b {
		t.Fatalf("commit gave %+v", c1)
	}
	var f git.File
	repo("readFile", map[string]any{"ref": b, "path": "posts/castles/index.md"}, &f)
	if f.Content != body || f.Encoding != "text" || f.Size != int64(len(body)) || f.SHA == "" {
		t.Fatalf("readFile gave %+v", f)
	}
	repo("readFile", map[string]any{"ref": c1.SHA, "path": "posts/bin.dat"}, &f)
	if f.Encoding != "base64" || f.Content != "AAEC/w==" {
		t.Fatalf("readFile of binary content gave %+v", f)
	}
	if _, err := call(git.RepoRef, "readFile", map[string]any{"ref": b, "path": "posts/missing.md"}); code(err) != connectors.CodeNotFound {
		t.Fatalf("readFile of a missing file: %v, want notFound", err)
	}
	if _, err := call(git.RepoRef, "commit", map[string]any{"branch": b, "message": "x", "changes": []map[string]any{{"path": "../etc/passwd", "content": "x"}}}); code(err) != connectors.CodeInvalid {
		t.Fatalf("a commit outside the repository: %v, want invalid", err)
	}

	// readTree lists children, recursively, and in pages
	var tree git.Tree
	repo("readTree", map[string]any{"ref": b, "path": "posts"}, &tree)
	if got := kinds(tree.Entries); got != "posts/bin.dat:file posts/castles:dir" {
		t.Fatalf("readTree posts gave %s", got)
	}
	var all []git.Entry
	cursor := ""
	for pages := 0; ; pages++ {
		var page git.Tree
		repo("readTree", map[string]any{"ref": b, "path": "posts", "recursive": true, "limit": 1, "cursor": cursor}, &page)
		all = append(all, page.Entries...)
		if page.Next == "" {
			break
		}
		if pages > 10 {
			t.Fatal("readTree's pages do not end")
		}
		cursor = page.Next
	}
	if got := kinds(all); got != "posts/bin.dat:file posts/castles/index.md:file posts/castles/notes.txt:file" {
		t.Fatalf("readTree recursive, one per page, gave %s", got)
	}
	if _, err := call(git.RepoRef, "readTree", map[string]any{"ref": b, "path": "nowhere"}); code(err) != connectors.CodeNotFound {
		t.Fatalf("readTree of a missing folder: %v, want notFound", err)
	}

	// compare names the changes; a stale expectHead is refused
	var c2 git.CommitResult
	repo("commit", map[string]any{"branch": b, "expectHead": c1.SHA, "message": "Edit and remove", "changes": []map[string]any{
		{"path": "posts/castles/index.md", "content": body + "More.\n"},
		{"path": "posts/castles/notes.txt", "delete": true},
	}}, &c2)
	if c2.Before != c1.SHA || c2.Created {
		t.Fatalf("the second commit gave %+v", c2)
	}
	var cmp git.Comparison
	repo("compare", map[string]any{"base": c1.SHA, "head": b}, &cmp)
	if got := changes(cmp.Changes); got != "posts/castles/index.md:modified posts/castles/notes.txt:deleted" || cmp.Head != c2.SHA {
		t.Fatalf("compare gave %s (head %s)", got, cmp.Head)
	}
	if _, err := call(git.RepoRef, "commit", map[string]any{"branch": b, "expectHead": c1.SHA, "message": "Stale", "changes": []map[string]any{{"path": "stale.md", "content": "x"}}}); code(err) != connectors.CodeConflict {
		t.Fatalf("a commit with a stale expectHead: %v, want conflict", err)
	}

	// A retried keyed commit makes one commit
	key := "suite-key-" + o.Run + b
	keyed := map[string]any{"branch": b, "message": "Keyed", "key": key, "changes": []map[string]any{{"path": "keyed.md", "content": "once"}}}
	var k1, k2 git.CommitResult
	repo("commit", keyed, &k1)
	repo("commit", keyed, &k2)
	if k1.SHA != k2.SHA {
		t.Fatalf("a retried keyed commit made another: %s then %s", k1.SHA, k2.SHA)
	}
	var log struct{ Refs []git.Ref }
	repo("listRefs", map[string]any{"prefix": "refs/heads/" + o.Prefix}, &log)
	if !hasRef(log.Refs, "refs/heads/"+b, k1.SHA) {
		t.Fatalf("listRefs gave %+v, want %s at %s", log.Refs, b, k1.SHA)
	}

	// Changes that change nothing make no commit
	var same git.CommitResult
	repo("commit", map[string]any{"branch": b, "message": "Nothing", "changes": []map[string]any{{"path": "keyed.md", "content": "once"}}}, &same)
	if !same.Unchanged || same.SHA != k1.SHA {
		t.Fatalf("an empty commit gave %+v", same)
	}

	if o.Push != nil && o.Watch != nil {
		pushed(t, o, main)
	}
	if o.Review {
		review(t, call, b, main)
	}
}

// pushed checks an outside push arrives once.
func pushed(t *testing.T, o Options, main string) {
	t.Helper()
	got := make(chan adapter.Event, 64)
	stop := o.Watch(t, func(e adapter.Event) { got <- e })
	defer stop()
	target := o.Prefix + "pushed" + o.Run
	sha, err := o.Push(target)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	seen := 0
	deadline := time.After(10 * time.Second)
	quiet := time.After(time.Hour)
	for {
		select {
		case e := <-got:
			if e.Name != git.EventPush {
				continue
			}
			var p git.Pushed
			_ = json.Unmarshal(e.Payload, &p)
			if p.Ref == "refs/heads/"+target && p.After == sha {
				seen++
				if len(p.Paths) == 0 {
					t.Errorf("pushed names no paths: %s", e.Payload)
				}
				quiet = time.After(300 * time.Millisecond)
			}
		case <-quiet:
			if seen != 1 {
				t.Fatalf("pushed arrived %d times, want once", seen)
			}
			return
		case <-deadline:
			if seen == 1 {
				return
			}
			t.Fatalf("pushed for %s at %s arrived %d times, want once", target, sha, seen)
		}
	}
}

// review checks pull requests open once, read and list.
func review(t *testing.T, call Caller, head, base string) {
	t.Helper()
	open := map[string]any{"head": head, "base": base, "title": "Suite: a post", "body": "From the contract suite.", "draft": true}
	var p1, p2, got git.PullRequest
	for i, into := range []*git.PullRequest{&p1, &p2} {
		raw, err := call(git.ReviewRef, "openPullRequest", open)
		if err != nil {
			t.Fatalf("openPullRequest %d: %v", i+1, err)
		}
		_ = json.Unmarshal(raw, into)
	}
	if p1.Number == 0 || p1.Number != p2.Number || p1.State != "open" || p1.Head != head || p1.Base != base {
		t.Fatalf("openPullRequest twice gave %+v then %+v", p1, p2)
	}
	raw, err := call(git.ReviewRef, "getPullRequest", map[string]any{"number": p1.Number})
	if err != nil {
		t.Fatalf("getPullRequest: %v", err)
	}
	_ = json.Unmarshal(raw, &got)
	if got.Title != "Suite: a post" || !got.Draft {
		t.Fatalf("getPullRequest gave %+v", got)
	}
	raw, err = call(git.ReviewRef, "listPullRequests", map[string]any{"head": head})
	if err != nil {
		t.Fatalf("listPullRequests: %v", err)
	}
	var list struct{ PullRequests []git.PullRequest }
	_ = json.Unmarshal(raw, &list)
	if len(list.PullRequests) != 1 || list.PullRequests[0].Number != p1.Number {
		t.Fatalf("listPullRequests gave %+v", list.PullRequests)
	}
}

func kinds(es []git.Entry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Path+":"+e.Kind)
	}
	return strings.Join(out, " ")
}

func changes(cs []git.Change) string {
	var out []string
	for _, c := range cs {
		out = append(out, fmt.Sprintf("%s:%s", c.Path, c.Status))
	}
	return strings.Join(out, " ")
}

func hasRef(refs []git.Ref, name, sha string) bool {
	for _, r := range refs {
		if r.Name == name && r.SHA == sha {
			return true
		}
	}
	return false
}
