package gitnodes_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/gittest"
	gitnodes "github.com/SgtFlame/foundation/backend/features/connectors/git/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

const tenant = 7

// world is the Git test server behind a Caller, as the dispatcher would
// call it: "site" protects nothing; "open" allows the default branch.
type world struct {
	s     *gittest.Server
	sess  map[string]adapter.Session
	reg   *extensions.Registry
	calls []string
}

func setup(t *testing.T) *world {
	t.Helper()
	w := &world{s: gittest.New(nil), sess: map[string]adapter.Session{}, reg: extensions.NewRegistry()}
	for conn, settings := range map[string]string{"site": `{}`, "open": `{"allowDefaultBranch":true}`} {
		ctx := adapter.Context{Context: context.Background(), Args: args(), Connection: conn}
		sess, err := w.s.Impl().Open(ctx, adapter.Connection{ID: conn, Type: gittest.TypeID, Tenant: tenant, Settings: json.RawMessage(settings)})
		if err != nil {
			t.Fatal(err)
		}
		w.sess[conn] = sess
	}
	w.reg.Declare(cognition.Nodes, tools.Tools)
	deps := func() gitnodes.Deps {
		return gitnodes.Deps{Call: w.call, Repositories: func(features.ServiceArgs) ([]gitnodes.Repository, error) {
			return []gitnodes.Repository{{ID: "site", Name: "Site", Type: gittest.TypeID, Status: "ok"}}, nil
		}}
	}
	gitnodes.Contribute(w.reg, deps)
	gitnodes.ContributeTools(w.reg, deps)
	return w
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, TenantID: tenant, UserID: 2}}
}

func (w *world) call(a features.ServiceArgs, c connectors.Call) (json.RawMessage, *connectors.Error) {
	sess, ok := w.sess[c.Connection]
	if !ok {
		return nil, connectors.Errorf(connectors.CodeNotFound, "no connection %s", c.Connection)
	}
	w.calls = append(w.calls, c.Operation)
	out, err := sess.Call(adapter.Context{Context: a.Context(), Args: a, Contract: c.Contract, Operation: c.Operation, Connection: c.Connection}, c.Operation, c.Args)
	if err != nil {
		var ce *connectors.Error
		if e, ok := err.(*connectors.Error); ok {
			ce = e
		} else {
			ce = connectors.Errorf(connectors.CodeUnavailable, "%v", err)
		}
		return nil, ce
	}
	return out, nil
}

func (w *world) node(t *testing.T, id string) cognition.NodeType {
	t.Helper()
	all := cognition.Nodes.All(w.reg)
	i := slices.IndexFunc(all, func(n cognition.NodeType) bool { return n.ID == id })
	if i < 0 {
		t.Fatalf("no node %s", id)
	}
	return all[i]
}

func (w *world) tool(t *testing.T, id string, a any) (tools.Result, error) {
	t.Helper()
	all := tools.Tools.All(w.reg)
	i := slices.IndexFunc(all, func(tl tools.Tool) bool { return tl.ID == id })
	if i < 0 {
		t.Fatalf("no tool %s", id)
	}
	raw, _ := json.Marshal(a)
	return all[i].Run(tools.Call{ServiceArgs: args(), ID: "call-1", Name: id, Args: raw})
}

func exec(nodeID string) *cognition.Exec {
	a := args()
	a.Meta.RunID, a.Meta.NodeID = 99, nodeID
	return &cognition.Exec{ServiceArgs: a}
}

func (w *world) log(t *testing.T, conn, branch string) []string {
	t.Helper()
	raw, err := w.sess[conn].Call(adapter.Context{Context: context.Background(), Args: args(), Contract: gittest.ControlRef, Operation: "log", Connection: conn}, "log", json.RawMessage(`{"branch":"`+branch+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var l struct{ Commits []struct{ Message string } }
	_ = json.Unmarshal(raw, &l)
	var out []string
	for _, c := range l.Commits {
		out = append(out, strings.SplitN(c.Message, "\n", 2)[0])
	}
	return out
}

func TestNodes(t *testing.T) {
	w := setup(t)
	commit := w.node(t, gitnodes.NodeCommit)
	in := cognition.Values{"connection": "site", "branch": "docs/post", "message": "Add the post",
		"changes": []any{map[string]any{"path": "www/content/code/x/index.md", "content": "# X\n"}}}
	out, pin, err := commit.Run(exec("n1"), in)
	if err != nil || pin != cognition.Then || out["created"] != true || out["sha"] == "" {
		t.Fatalf("commit: %v %s %v", out, pin, err)
	}
	// The same node in the same run writing the same changes: one commit
	again, _, _ := commit.Run(exec("n1"), in)
	if again["sha"] != out["sha"] || len(w.log(t, "site", "docs/post")) != 2 {
		t.Fatalf("a retried commit: %v, log %v", again, w.log(t, "site", "docs/post"))
	}
	// Inside a job the key follows the job, whatever the run
	jx := exec("n1")
	jx.ServiceArgs.Ctx = jobs.WithInfo(context.Background(), jobs.Info{ID: "job-1"})
	jx.ServiceArgs.Meta.RunID = 100
	in2 := cognition.Values{"connection": "site", "branch": "docs/post", "message": "Second", "changes": []any{map[string]any{"path": "b.md", "content": "b"}}}
	first, _, _ := commit.Run(jx, in2)
	jx.ServiceArgs.Meta.RunID = 101
	second, _, _ := commit.Run(jx, in2)
	if first["sha"] != second["sha"] || len(w.log(t, "site", "docs/post")) != 3 {
		t.Fatalf("a retried job: %v %v", first, second)
	}
	// The default branch: refused unless the connection allows it
	out, pin, err = commit.Run(exec("n2"), cognition.Values{"connection": "site", "branch": "main", "message": "x", "changes": []any{map[string]any{"path": "a.md", "content": "a"}}})
	if err != nil || pin != gitnodes.Failed || out["error"].(map[string]any)["code"] != connectors.CodeInvalid {
		t.Fatalf("main without allowDefaultBranch: %v %s %v", out, pin, err)
	}
	if _, pin, err = commit.Run(exec("n2"), cognition.Values{"connection": "open", "branch": "main", "message": "x", "changes": []any{map[string]any{"path": "a.md", "content": "a"}}}); pin != cognition.Then || err != nil {
		t.Fatalf("main with allowDefaultBranch: %s %v", pin, err)
	}
	// Tree and file
	tree, pin, _ := w.node(t, gitnodes.NodeTree).Run(exec("n3"), cognition.Values{"connection": "site", "ref": "docs/post", "recursive": true})
	if pin != cognition.Then || len(tree["entries"].([]any)) != 3 {
		t.Fatalf("tree: %v", tree)
	}
	f, pin, _ := w.node(t, gitnodes.NodeReadFile).Run(exec("n4"), cognition.Values{"connection": "site", "ref": "docs/post", "path": "www/content/code/x/index.md"})
	if pin != cognition.Then || f["content"] != "# X\n" || f["encoding"] != "text" {
		t.Fatalf("file: %v", f)
	}
	if f, pin, _ = w.node(t, gitnodes.NodeReadFile).Run(exec("n4"), cognition.Values{"connection": "site", "path": "nope.md"}); pin != gitnodes.Failed {
		t.Fatalf("a missing file: %v", f)
	}
	// A pull request, once
	pr := w.node(t, gitnodes.NodePullRequest)
	p1, pin, _ := pr.Run(exec("n5"), cognition.Values{"connection": "site", "head": "docs/post", "title": "Publish"})
	p2, _, _ := pr.Run(exec("n5"), cognition.Values{"connection": "site", "head": "docs/post", "title": "Publish"})
	if pin != cognition.Then || p1["number"] != p2["number"] || p1["state"] != "open" {
		t.Fatalf("pull request: %v %v", p1, p2)
	}
}

// A retryable error fails the run, so a job retries it.
func TestRetryable(t *testing.T) {
	w := setup(t)
	_, err := w.sess["site"].Call(adapter.Context{Context: context.Background(), Args: args(), Contract: gittest.ControlRef, Operation: "fail", Connection: "site"}, "fail", json.RawMessage(`{"operation":"readTree","kind":"unavailable"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.node(t, gitnodes.NodeTree).Run(exec("n"), cognition.Values{"connection": "site"}); err == nil {
		t.Fatal("an unavailable host left by Error")
	}
}

func TestTools(t *testing.T) {
	w := setup(t)
	res, err := w.tool(t, gitnodes.ToolRepositories, map[string]any{})
	if err != nil || len(res.Content["repositories"].([]gitnodes.Repository)) != 1 {
		t.Fatalf("repositories: %v %v", res, err)
	}
	if _, err := w.tool(t, gitnodes.ToolCommit, map[string]any{"connection": "site", "branch": "main", "message": "x", "changes": []any{map[string]any{"path": "a.md", "content": "a"}}}); err == nil || !strings.Contains(err.Error(), "allowDefaultBranch") {
		t.Fatalf("a commit to main: %v", err)
	}
	res, err = w.tool(t, gitnodes.ToolCommit, map[string]any{"connection": "site", "branch": "docs/a", "message": "Add a", "changes": []any{map[string]any{"path": "a.md", "content": "a"}}})
	if err != nil || res.Content["created"] != true {
		t.Fatalf("commit: %v %v", res, err)
	}
	// The same call again (a retry) commits once
	if _, err := w.tool(t, gitnodes.ToolCommit, map[string]any{"connection": "site", "branch": "docs/a", "message": "Add a", "changes": []any{map[string]any{"path": "a.md", "content": "a"}}}); err != nil || len(w.log(t, "site", "docs/a")) != 2 {
		t.Fatalf("a retried call: %v %v", err, w.log(t, "site", "docs/a"))
	}
	res, err = w.tool(t, gitnodes.ToolTree, map[string]any{"connection": "site", "ref": "docs/a"})
	if err != nil || len(res.Content["entries"].([]any)) != 2 {
		t.Fatalf("tree: %v %v", res, err)
	}
	res, err = w.tool(t, gitnodes.ToolRead, map[string]any{"connection": "site", "ref": "docs/a", "path": "a.md"})
	if err != nil || res.Content["content"] != "a" {
		t.Fatalf("read: %v %v", res, err)
	}
	res, err = w.tool(t, gitnodes.ToolPullRequest, map[string]any{"connection": "site", "head": "docs/a", "title": "Add a"})
	if err != nil || res.Content["number"] == nil || !strings.Contains(res.Summary, "docs/a") {
		t.Fatalf("pull request: %v %v", res, err)
	}
	for _, tl := range tools.Tools.All(w.reg) {
		if tl.Description == "" || len(tl.Input) == 0 || (tl.Effect != tools.EffectRead && tl.Effect != tools.EffectWrite) {
			t.Errorf("tool %s is not described", tl.ID)
		}
	}
	if _, err := w.tool(t, gitnodes.ToolRead, map[string]any{"connection": "gone", "path": "a.md"}); err == nil || !strings.HasPrefix(err.Error(), "notFound") {
		t.Fatalf("an unknown connection: %v", err)
	}
	_ = git.RepoRef
}
