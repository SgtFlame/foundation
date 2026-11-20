package installs_test

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	apptools "github.com/SgtFlame/foundation/backend/features/apps/tools"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	authzimpl "github.com/SgtFlame/foundation/backend/features/authz/services/authzservice/impl"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// readTask is a function graph the Tasks app gives as the tool readTask:
// the task's title, or nothing when the caller cannot see it.
func readTask() map[string]any {
	return map[string]any{"name": "Read a task", "kind": "function", "owner": "app:tasks",
		"nodes": []any{
			node("event", cognition.EventCall, map[string]any{"outputs": []any{map[string]any{"id": "id", "type": "string", "description": "The task's id"}}}),
			node("get", "records.get", map[string]any{"entity": "task"}),
			node("ret", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "task", "type": "json"}, map[string]any{"id": "exists", "type": "bool"}}}),
		},
		"edges": edges("event.then -> ret.then", "event.id -> get.id", "get.record -> ret.task", "get.exists -> ret.exists"),
	}
}

// renameTask is a function graph the Tasks app gives as the write tool
// renameTask: it sends record.update as the caller.
func renameTask() map[string]any {
	return map[string]any{"name": "Rename a task", "kind": "function", "owner": "app:tasks",
		"nodes": []any{
			node("event", cognition.EventCall, map[string]any{"outputs": []any{
				map[string]any{"id": "id", "type": "string", "description": "The task's id"}, map[string]any{"id": "title", "type": "string", "description": "Its new title"}}}),
			node("set", "data.expression", map[string]any{"expr": `{entity: "task", set: {title: title}}`, "type": "json"}),
			node("send", "live.sendCommand", map[string]any{"command": "record.update"}),
			node("ok", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "renamed", "type": "bool"}}}),
			node("no", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "error", "type": "string"}}}),
			node("yes", "data.expression", map[string]any{"expr": `true`, "type": "bool"}),
		},
		"edges": edges("event.then -> send.then", "event.id -> send.target", "event.title -> set.title", "set.value -> send.args",
			"send.ok -> ok.then", "yes.value -> ok.renamed", "send.rejected -> no.then", "send.message -> no.error"),
	}
}

// Agents (forge 07j): an agent record mints a principal; a role granted on
// one task lets the agent read and change that task, and no other, through
// a query, a command and the app's tools; revoking it fails the next call;
// deleting the record retires the agent; a sandboxed run as the agent
// commits nothing.
func TestAgentPrincipals(t *testing.T) {
	f := setup(t)
	f.tasks(true)
	own := "app:tasks"
	f.ok("entity.create", "bot", map[string]any{"name": "Bot", "owner": own, "titleField": "name", "fields": []map[string]any{
		{"name": "name", "type": "text"}, {"name": "principal", "type": "principal", "kind": "agent"}}})
	res := f.ok("entity.publish", "bot", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.bot", res.Seq)
	f.publishGraph("fn-tasks-readTask", readTask())
	f.publishGraph("fn-tasks-renameTask", renameTask())
	f.ok("app.addEntity", "tasks", map[string]any{"entity": "bot"})
	f.ok("app.addRole", "tasks", map[string]any{"id": "sender", "title": "Sender", "actions": []string{"entity:task.read", "entity:task.update"}})
	f.ok("app.addTool", "tasks", map[string]any{"name": "readTask", "graph": "fn-tasks-readTask", "description": "Reads a task by its id"})
	seq := f.ok("app.addTool", "tasks", map[string]any{"name": "renameTask", "graph": "fn-tasks-renameTask", "description": "Renames a task", "effect": "write"}).Seq
	f.publish(seq)

	pol, _ := f.access()
	authz.ContributeAgents(f.reg)
	authz.SetAgents(data.Agents{Deps: func() data.Deps { return *f.data }})
	t.Cleanup(func() { authz.SetAgents(nil) })
	f.lifecycle("app.install", "tasks", map[string]any{})
	eventually(t, "the app's roles", func() bool { return len(appRoles(t, pol, "tasks")) >= 1 })
	eventually(t, "the records under the app", func() bool {
		return strings.HasPrefix(authz.RecordResource(recordsResource("task"), "1"), "tenant:1/app:tasks/")
	})

	// The agent record mints the principal: its id the record's
	made := f.doAs(owner, "record.create", "", map[string]any{"entity": "bot", "values": map[string]any{"name": "Triage"}})
	if made.Common.Code >= 300 {
		t.Fatalf("the agent record: %s %+v", made.Common.Message, made.Problems)
	}
	f.wait(data.Projector, "bot."+made.Target, made.Seq)
	agentID, _ := strconv.ParseUint(made.Target, 10, 64)
	info, ok, err := authz.FindAgent(context.Background(), 1, agentID)
	if err != nil || !ok || info.Name != "Triage" || info.CreatedBy != owner || info.Resource != "tenant:1/app:tasks/entity:bot/record:"+made.Target {
		t.Fatalf("the agent: %+v %v %v", info, ok, err)
	}
	if res := f.doAs(owner, "record.update", made.Target, map[string]any{"entity": "bot", "set": map[string]any{"principal": "9"}}); res.Common.Code < 300 {
		t.Error("a command set the principal field")
	}
	// Renamed with its record
	f.wait(data.Projector, "bot."+made.Target, f.doAs(owner, "record.update", made.Target, map[string]any{"entity": "bot", "set": map[string]any{"name": "Triage bot"}}).Seq)
	if info, _, _ = authz.FindAgent(context.Background(), 1, agentID); info.Name != "Triage bot" {
		t.Errorf("the agent was not renamed: %q", info.Name)
	}

	t1 := f.doAs(owner, "record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "Support", "assignee": "1"}})
	t2 := f.doAs(owner, "record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "Billing", "assignee": "1"}})
	f.wait(data.Projector, "task."+t2.Target, t2.Seq)

	// Only who made it, or holds agent.use on it, runs as it
	if _, err := authz.AsAgent(context.Background(), as(eve).Principal, agentID); err == nil {
		t.Error("Eve ran as the owner's agent")
	}
	agentP, err := authz.AsAgent(context.Background(), as(owner).Principal, agentID)
	if err != nil {
		t.Fatal(err)
	}
	agent := features.ServiceArgs{Ctx: context.Background(), Principal: agentP}

	// Nothing before a grant, though its creator is the Owner
	if _, code := f.idsAs(agent); code != authz.Status {
		t.Errorf("the agent read tasks before a grant: %d", code)
	}

	// Sender granted on Support alone, through the Share dialog's service
	az := authzimpl.New(authzimpl.WithPolicy(pol))
	sender := appRoles(t, pol, "tasks")["sender"]
	support := authz.RecordResource(recordsResource("task"), t1.Target)
	granted := az.Grant(authzservice.GrantArgs{ServiceArgs: as(owner), AgentID: agentID, RoleID: sender.ID, Resource: support})
	if granted.Common.Code >= 300 {
		t.Fatalf("grant: %s", granted.Common.Message)
	}
	if denied := az.Grant(authzservice.GrantArgs{ServiceArgs: as(eve), AgentID: agentID, RoleID: sender.ID, Resource: support}); denied.Common.Code != authz.Status {
		t.Errorf("Eve granted the agent a role: %d", denied.Common.Code)
	}
	if listed := az.Agents(authzservice.AgentsArgs{ServiceArgs: as(owner)}); len(listed.Agents) != 1 || listed.Agents[0].ID != agentID {
		t.Errorf("the Share dialog's agents: %+v", listed)
	}

	// A query
	if got, code := f.idsAs(agent); code != 0 || !slices.Equal(got, []string{t1.Target}) {
		t.Errorf("the agent sees %v (%d)", got, code)
	}
	// A command, its event naming the agent and whom it acted for
	up := f.exec(agent, "record.update", t1.Target, map[string]any{"entity": "task", "set": map[string]any{"title": "Support (triaged)"}})
	if up.Common.Code >= 300 {
		t.Errorf("the agent cannot change its task: %s %+v", up.Common.Message, up.Problems)
	}
	if res := f.exec(agent, "record.update", t2.Target, map[string]any{"entity": "task", "set": map[string]any{"title": "Mine"}}); res.Common.Code < 300 {
		t.Error("the agent changed a task it holds no role on")
	}
	// The app's tools, as the agent
	readT, renameT := f.tool(agent, "tasks.readTask"), f.tool(agent, "tasks.renameTask")
	if out, err := readT.Run(tools.Call{ServiceArgs: agent, Args: json.RawMessage(`{"id":"` + t1.Target + `"}`)}); err != nil || out.Content["exists"] != true {
		t.Errorf("readTask on its task: %v %v", out.Content, err)
	}
	if out, err := readT.Run(tools.Call{ServiceArgs: agent, Args: json.RawMessage(`{"id":"` + t2.Target + `"}`)}); err != nil || out.Content["exists"] != false {
		t.Errorf("readTask on another task: %v %v", out.Content, err)
	}
	if out, err := renameT.Run(tools.Call{ServiceArgs: agent, Args: json.RawMessage(`{"id":"` + t2.Target + `","title":"Mine"}`)}); err != nil || out.Content["renamed"] == true {
		t.Errorf("renameTask on another task: %v %v", out.Content, err)
	}
	if !renameT.AsksFor(nil) || readT.AsksFor(nil) || !slices.Contains(renameT.Input, '"') || !strings.Contains(string(renameT.Input), `"title"`) {
		t.Errorf("the tools' effects or schema: %s", renameT.Input)
	}
	// As a user in the chat it runs as them: the owner renames Billing
	if out, err := f.tool(as(owner), "tasks.renameTask").Run(tools.Call{ServiceArgs: as(owner), Args: json.RawMessage(`{"id":"` + t2.Target + `","title":"Billing 2"}`)}); err != nil || out.Content["renamed"] != true {
		t.Errorf("renameTask as the owner: %v %v", out.Content, err)
	}

	// Run in sandbox: as the agent, its change decided, nothing committed
	sres, err := apptools.Sandbox(as(owner), f.reg, apptools.ForgeDeps{Store: f.p, Bus: f.bus, Engine: f.eng}, apptools.TestArgs{
		Graph: "fn-tasks-renameTask", Agent: made.Target, Inputs: map[string]any{"id": t1.Target, "title": "Sandboxed"}})
	if err != nil || len(sres.Events) == 0 || sres.Outputs["renamed"] != true {
		t.Errorf("the sandboxed run: %+v %v", sres, err)
	}
	if title := f.title(t1.Target); title != "Support (triaged)" {
		t.Errorf("the sandbox committed: %q", title)
	}

	// Revoking fails the next call
	if r := az.Revoke(authzservice.RevokeArgs{ServiceArgs: as(owner), GrantID: granted.Grant.ID}); r.Common.Code >= 300 {
		t.Fatal(r.Common.Message)
	}
	if res := f.exec(agent, "record.update", t1.Target, map[string]any{"entity": "task", "set": map[string]any{"title": "Again"}}); res.Common.Code < 300 {
		t.Error("a revoked agent still changed its task")
	}

	// Deleting the record retires the agent
	az.Grant(authzservice.GrantArgs{ServiceArgs: as(owner), AgentID: agentID, RoleID: sender.ID, Resource: support})
	del := f.doAs(owner, "record.delete", made.Target, map[string]any{"entity": "bot"})
	f.wait(data.Projector, "bot."+made.Target, del.Seq)
	if _, ok, _ := authz.FindAgent(context.Background(), 1, agentID); ok {
		t.Error("the deleted record's agent is live")
	}
	if _, code := f.idsAs(agent); code != authz.Status {
		t.Errorf("a retired agent read tasks: %d", code)
	}
}

func recordsResource(entity string) string {
	return records.Resource(features.Principal{Kind: features.PrincipalUser, UserID: owner, TenantID: 1}, entity)
}

// exec sends a command as any principal.
func (f *fixture) exec(sa features.ServiceArgs, typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: sa, ID: "x" + strconv.FormatInt(f.n.Add(1), 10), Type: typ, Target: target, Args: raw})
}

// idsAs is ids for any principal.
func (f *fixture) idsAs(sa features.ServiceArgs) ([]string, int) {
	raw, _ := json.Marshal(map[string]any{"entity": "task"})
	v, _, fail := f.subs.Once(sa, "records.find", raw)
	if fail != nil {
		return nil, int(fail.Code)
	}
	b, _ := json.Marshal(v)
	var page struct{ Records []map[string]any }
	_ = json.Unmarshal(b, &page)
	var out []string
	for _, r := range page.Records {
		out = append(out, r["_id"].(string))
	}
	slices.Sort(out)
	return out, 0
}

// tool is an installed app's tool as the principal's round finds it.
func (f *fixture) tool(sa features.ServiceArgs, id string) tools.Tool {
	f.t.Helper()
	all := installs.InstalledTools(sa, f.reg, *f.install)
	i := slices.IndexFunc(all, func(t tools.Tool) bool { return t.ID == id })
	if i < 0 {
		f.t.Fatalf("no tool %s", id)
	}
	return all[i]
}

// title is a task's title as projected.
func (f *fixture) title(id string) string {
	v, _, _ := f.p.Get(context.Background(), 1, data.Projector, data.Key("task", id))
	var r map[string]any
	_ = json.Unmarshal(v.Value, &r)
	s, _ := r["title"].(string)
	return s
}
