package recordstools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordstools "github.com/SgtFlame/foundation/backend/features/records/tools"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// TestTheChatDefinesTask runs the tools a chat model gets: the entity.*
// commands' own tools, then entity.describe.
func TestTheChatDefinesTask(t *testing.T) {
	reg := extensions.NewRegistry()
	var deps entities.Deps
	var subs *subscribe.Subscriptions
	var toolDeps livetools.Deps
	entities.Contribute(reg, func() entities.Deps { return deps })
	recordstools.Contribute(reg, func() *subscribe.Subscriptions { return subs })
	livetools.Contribute(reg, func() livetools.Deps { return toolDeps })
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	deps = entities.Deps{Projections: p}
	subs = subscribe.New(p, reg)
	toolDeps = livetools.Deps{Bus: bus.New(p), Subscriptions: subs}

	run := func(name string, input any) tools.Result {
		t.Helper()
		for _, tool := range tools.Tools.All(reg) {
			if tool.ID != name {
				continue
			}
			raw, _ := json.Marshal(input)
			if err := tools.CheckArgs(tool.Input, raw); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			res, err := tool.Run(tools.Call{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}, Name: name, Args: raw})
			if err != nil {
				t.Fatalf("%s: %v %v", name, err, res.Content)
			}
			return res
		}
		t.Fatalf("there is no tool %s", name)
		return tools.Result{}
	}

	if res := run("entity.listFieldTypes", map[string]any{}); len(fmt.Sprint(res.Content["fieldTypes"])) < 100 {
		t.Errorf("field types = %v", res.Content)
	}
	res := run("entity.create", map[string]any{"target": "task", "args": map[string]any{"name": "Task", "fields": []any{
		map[string]any{"name": "title", "type": "text", "required": true},
		map[string]any{"name": "done", "type": "bool"},
	}}})
	if report, _ := res.Content["report"].(map[string]any); report["rev"] != 1 || report["fields"] != 2 {
		t.Errorf("create's report = %v", res.Content)
	}
	run("entity.addField", map[string]any{"target": "task", "args": map[string]any{"name": "due", "type": "date"}})
	run("entity.addField", map[string]any{"target": "task", "args": map[string]any{"name": "assignee", "type": "relation", "relation": map[string]any{"entity": "user"}}})

	var view entities.View
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline) && len(view.Entity.Fields) < 4; time.Sleep(20 * time.Millisecond) {
		raw, _ := json.Marshal(run("entity.describe", map[string]any{"id": "task"}).Content["entityType"])
		_ = json.Unmarshal(raw, &view)
	}
	if len(view.Entity.Fields) != 4 || view.Entity.Fields[3].Relation.Entity != "user" || view.Entity.TitleField != "title" {
		t.Errorf("described = %+v", view.Entity)
	}
	if list := run("entity.list", map[string]any{}).Content["entityTypes"].([]any); len(list) != 1 {
		t.Errorf("list = %v", list)
	}
}
