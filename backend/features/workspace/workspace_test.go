package workspace_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/workspace"
	"github.com/nats-io/nats.go/jetstream"
)

// process is one runner over the shared JetStream.
type process struct {
	reg  *extensions.Registry
	p    *project.Projections
	bus  *bus.Bus
	subs *subscribe.Subscriptions
}

func newProcess(t *testing.T, js jetstream.JetStream) *process {
	reg := extensions.NewRegistry()
	pr := &process{reg: reg}
	workspace.ContributeAccess(reg)
	workspace.Contribute(reg, func() workspace.Deps { return workspace.Deps{Store: pr.p, Bus: pr.bus} })
	livetools.Contribute(reg, func() livetools.Deps { return livetools.Deps{Bus: pr.bus, Subscriptions: pr.subs} })
	pr.p = project.New(js, reg)
	t.Cleanup(pr.p.Close)
	pr.bus = bus.New(pr.p)
	pr.subs = subscribe.New(pr.p, reg)
	return pr
}

func setup(t *testing.T, processes int) []*process {
	srv := natsd.StartTest(t)
	var out []*process
	for range processes {
		out = append(out, newProcess(t, srv.JS()))
	}
	return out
}

func as(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}
}

var ana, bo = as(7), as(8)

// list is the workspace.list snapshot args' principal reads on pr.
func list(t *testing.T, pr *process, args features.ServiceArgs) []workspace.Workspace {
	t.Helper()
	data, _, fail := pr.subs.Once(args, "workspace.list", nil)
	if fail != nil {
		t.Fatalf("workspace.list: %s", fail.Message)
	}
	raw, _ := json.Marshal(data)
	var v struct{ Workspaces []workspace.Workspace }
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v.Workspaces
}

func names(ws []workspace.Workspace) string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Name)
	}
	return strings.Join(out, ",")
}

func cmd(pr *process, args features.ServiceArgs, typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return pr.bus.Execute(bus.Request{ServiceArgs: args, ID: "c" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""), Type: typ, Target: target, Args: raw})
}

func ok(t *testing.T, res bus.Result) bus.Result {
	t.Helper()
	if res.Common.Code >= 300 {
		t.Fatalf("refused (%d %s): %s %+v", res.Common.Code, res.Common.ErrorCode, res.Common.Message, res.Problems)
	}
	return res
}

func refused(t *testing.T, res bus.Result, code string) {
	t.Helper()
	if res.Common.Code < 300 || res.Common.ErrorCode != code {
		t.Fatalf("want %s, got %d %s: %s", code, res.Common.Code, res.Common.ErrorCode, res.Common.Message)
	}
}

func settle(t *testing.T, pr *process) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pr.p.Settle(ctx, 1, workspace.Projector); err != nil {
		t.Fatal(err)
	}
}

// Two processes reading a person's workspaces at once for the first time
// make one Default between them.
func TestDefaultIsMadeOnceUnderConcurrentReads(t *testing.T) {
	procs := setup(t, 2)
	var wg sync.WaitGroup
	got := make([][]workspace.Workspace, 4)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = list(t, procs[i%2], ana)
		}()
	}
	wg.Wait()
	for i, ws := range got {
		if len(ws) != 1 || ws[0].ID != workspace.DefaultID || ws[0].Name != "Default" || !ws[0].IsDefault {
			t.Fatalf("read %d: %+v", i, ws)
		}
	}
	settle(t, procs[0])
	all, err := workspace.All(context.Background(), procs[0].p, 1, 7)
	if err != nil || len(all) != 1 || all[0].Version != 1 {
		t.Fatalf("stored %+v %v", all, err)
	}
}

// A create sent twice with one id makes one workspace; names are unique
// per person, trimmed and short; positions follow in order.
func TestCreateRenameAndNames(t *testing.T) {
	pr := setup(t, 1)[0]
	list(t, pr, ana)
	ok(t, cmd(pr, ana, "workspace.create", "w1", map[string]any{"name": "  Support  "}))
	ok(t, cmd(pr, ana, "workspace.create", "w1", map[string]any{"name": "Support"}))
	refused(t, cmd(pr, ana, "workspace.create", "w2", map[string]any{"name": "support!"}), "workspace_name_taken")
	refused(t, cmd(pr, ana, "workspace.create", "w2", map[string]any{"name": strings.Repeat("x", 61)}), "command_rejected")
	refused(t, cmd(pr, ana, "workspace.create", "w2", map[string]any{"name": " -- "}), "command_rejected")
	ok(t, cmd(pr, ana, "workspace.create", "w3", map[string]any{"name": "Sales"}))
	settle(t, pr)
	ws := list(t, pr, ana)
	if names(ws) != "Default,Support,Sales" || ws[1].Position != 1 || ws[2].Position != 2 {
		t.Fatalf("workspaces %+v", ws)
	}
	ok(t, cmd(pr, ana, "workspace.rename", "w3", map[string]any{"name": "Leads"}))
	refused(t, cmd(pr, ana, "workspace.rename", "w3", map[string]any{"name": "default"}), "workspace_name_taken")
	refused(t, cmd(pr, ana, "workspace.rename", "nope", map[string]any{"name": "X"}), "workspace_not_found")
	// a create from the chat's tool, with no target, gets a new id
	res := ok(t, cmd(pr, ana, "workspace.create", "", map[string]any{"name": "Billing"}))
	if !strings.HasPrefix(res.Target, "w") {
		t.Fatalf("target %q", res.Target)
	}
	settle(t, pr)
	if got := names(list(t, pr, ana)); got != "Default,Support,Leads,Billing" {
		t.Fatalf("after rename: %s", got)
	}
}

// Duplicate copies the layout and context; update refuses a stale version;
// Default cannot be deleted, others can.
func TestDuplicateUpdateAndDelete(t *testing.T) {
	pr := setup(t, 1)[0]
	list(t, pr, ana)
	ok(t, cmd(pr, ana, "workspace.create", "w1", map[string]any{"name": "Support"}))
	settle(t, pr)
	w1 := list(t, pr, ana)[1]
	ok(t, cmd(pr, ana, "workspace.update", "w1", map[string]any{"version": w1.Version, "layout": map[string]any{"tabs": []string{"chat:main"}}, "context": map[string]any{"volumes": []string{"v1"}}}))
	refused(t, cmd(pr, ana, "workspace.update", "w1", map[string]any{"version": w1.Version, "layout": map[string]any{}}), "stale_workspace")
	ok(t, cmd(pr, ana, "workspace.create", "w2", map[string]any{"name": "Support copy", "from": "w1"}))
	refused(t, cmd(pr, ana, "workspace.create", "w3", map[string]any{"name": "Other", "from": "nope"}), "workspace_not_found")
	settle(t, pr)
	ws := list(t, pr, ana)
	if len(ws) != 3 || string(ws[2].Layout) != `{"tabs":["chat:main"]}` || string(ws[2].Context) != `{"volumes":["v1"]}` {
		t.Fatalf("copy %+v", ws)
	}
	refused(t, cmd(pr, ana, "workspace.delete", workspace.DefaultID, map[string]any{}), "default_workspace")
	ok(t, cmd(pr, ana, "workspace.delete", "w1", map[string]any{}))
	ok(t, cmd(pr, ana, "workspace.delete", "w1", map[string]any{})) // deleted already
	settle(t, pr)
	if got := names(list(t, pr, ana)); got != "Default,Support copy" {
		t.Fatalf("after delete: %s", got)
	}
}

// Move renumbers the person's workspaces in one commit.
func TestMove(t *testing.T) {
	pr := setup(t, 1)[0]
	list(t, pr, ana)
	for _, n := range []string{"A", "B", "C"} {
		ok(t, cmd(pr, ana, "workspace.create", "w"+n, map[string]any{"name": n}))
	}
	ok(t, cmd(pr, ana, "workspace.move", "wC", map[string]any{"position": 0}))
	settle(t, pr)
	ws := list(t, pr, ana)
	if names(ws) != "C,Default,A,B" {
		t.Fatalf("moved: %s", names(ws))
	}
	for i, w := range ws {
		if w.Position != i {
			t.Fatalf("positions %+v", ws)
		}
	}
	ok(t, cmd(pr, ana, "workspace.move", "wC", map[string]any{"position": 99}))
	settle(t, pr)
	if got := names(list(t, pr, ana)); got != "Default,A,B,C" {
		t.Fatalf("moved to the end: %s", got)
	}
}

// One person's workspaces are theirs alone: Bo cannot list, rename or
// delete Ana's, whatever id he names, and an agent or service has none.
func TestWorkspacesArePrivate(t *testing.T) {
	pr := setup(t, 1)[0]
	list(t, pr, ana)
	ok(t, cmd(pr, ana, "workspace.create", "w1", map[string]any{"name": "Support"}))
	settle(t, pr)
	if got := names(list(t, pr, bo)); got != "Default" {
		t.Fatalf("Bo sees %s", got)
	}
	refused(t, cmd(pr, bo, "workspace.rename", "w1", map[string]any{"name": "Mine"}), "workspace_not_found")
	ok(t, cmd(pr, bo, "workspace.delete", "w1", map[string]any{})) // nothing of his to delete
	// Bo making w1 makes his own
	ok(t, cmd(pr, bo, "workspace.create", "w1", map[string]any{"name": "Bo's"}))
	settle(t, pr)
	if got := names(list(t, pr, ana)); got != "Default,Support" {
		t.Fatalf("Ana's after Bo: %s", got)
	}
	agent := features.ServiceArgs{Ctx: context.Background(), Principal: features.AgentPrincipal(1, 77, 7)}
	if res := cmd(pr, agent, "workspace.create", "w9", map[string]any{"name": "X"}); res.Common.Code < 300 {
		t.Fatal("an agent made a workspace")
	}
	if _, _, fail := pr.subs.Once(agent, "workspace.list", nil); fail == nil {
		t.Fatal("an agent listed workspaces")
	}
}

// The live query follows changes made on another process.
func TestListFollowsAnotherProcess(t *testing.T) {
	procs := setup(t, 2)
	list(t, procs[0], ana)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sink := &changes{got: make(chan string, 8)}
	args := ana
	args.Ctx = ctx
	if fail := procs[0].subs.Open(subscribe.OpenArgs{ServiceArgs: args, Query: "workspace.list", Sink: sink}); fail != nil {
		t.Fatal(fail.Message)
	}
	<-sink.got // the snapshot
	ok(t, cmd(procs[1], ana, "workspace.create", "w1", map[string]any{"name": "Support"}))
	select {
	case v := <-sink.got:
		if !strings.Contains(v, `"Support"`) {
			t.Fatalf("change %s", v)
		}
	case <-ctx.Done():
		t.Fatal("no change")
	}
}

// The command tools and workspace.list are the model's.
func TestTools(t *testing.T) {
	pr := setup(t, 1)[0]
	list(t, pr, ana)
	run := func(name string, a any) tools.Result {
		t.Helper()
		for _, tool := range tools.Available(pr.reg, ana) {
			if tool.ID == name {
				raw, _ := json.Marshal(a)
				res, err := tool.Run(tools.Call{ServiceArgs: ana, Name: name, Args: raw})
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				return res
			}
		}
		t.Fatalf("no tool %s", name)
		return tools.Result{}
	}
	run("workspace.create", map[string]any{"args": map[string]any{"name": "Support"}})
	settle(t, pr)
	if res := run("workspace.list", map[string]any{}); res.Summary != "Workspaces: Default, Support" {
		t.Fatalf("list %+v", res)
	}
	for _, tool := range tools.Available(pr.reg, ana) {
		if tool.ID == "workspace.update" || tool.ID == "workspace.makeDefault" {
			t.Fatalf("%s is a tool", tool.ID)
		}
	}
}

type changes struct{ got chan string }

func (c *changes) Snapshot(data any, _ live.Position)   { c.send(data) }
func (c *changes) Change(d live.Delta, _ live.Position) { c.send(d) }
func (c *changes) Failed(features.ServiceResponse)      {}
func (c *changes) send(v any) {
	raw, _ := json.Marshal(v)
	c.got <- string(raw)
}
