package installs_test

import (
	"testing"

	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// An app's draft run (forge 05f question 3): its pages render the drafts
// of its prefabs and follow its manifest as it changes; its commands and
// queries are draft.<appId>.<name>, run by their graphs' drafts, for
// real, with nothing published or installed but the entity type.
func TestDraftRun(t *testing.T) {
	f := setup(t)
	f.tasks(false)
	res := f.ok("entity.publish", "task", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.task", res.Seq)
	f.ok("prefab.insertNode", "myTasks", map[string]any{"node": map[string]any{"type": "mui.heading", "key": "h", "props": map[string]any{"text": "Mine, drafted"}}})
	f.ok("prefab.insertNode", "taskDetail", map[string]any{"node": map[string]any{"type": "mui.text", "key": "id", "bind": map[string]any{"text": `="Task " + props.id`}}})
	f.ok("record.create", "t1", map[string]any{"entity": "task", "values": map[string]any{"title": "Write docs"}})

	// The page: the drafts, never published
	run := f.open("tasks/draft", map[string]any{})
	run.until("showing the draft of My tasks", func(p *page) bool { return p.shows("Mine, drafted") })
	if run.title != "Tasks (draft)" {
		t.Errorf("title %q", run.title)
	}
	// A change to a prefab's draft reaches the open page
	f.ok("prefab.updateNode", "myTasks", map[string]any{"path": "h", "props": map[string]any{"text": "Mine, edited"}})
	run.until("showing the edit", func(p *page) bool { return p.shows("Mine, edited") })
	// A page added to the draft is there to navigate to, in the same session
	f.ok("prefab.create", "allTasks", map[string]any{"name": "All tasks", "owner": "app:tasks"})
	f.ok("prefab.insertNode", "allTasks", map[string]any{"node": map[string]any{"type": "mui.heading", "key": "h", "props": map[string]any{"text": "Everyone's"}}})
	seq := f.ok("app.addPage", "tasks", map[string]any{"route": "/all", "prefab": "allTasks"}).Seq
	f.draft("tasks", seq)
	run.s.Event(args(), "app.navigate", nil, map[string]any{"route": "/all"})
	run.until("on the new page", func(p *page) bool { return p.shows("Everyone's") })
	run.s.Event(args(), "app.navigate", nil, map[string]any{"route": "/task/t1"})
	run.until("on a task", func(p *page) bool { return p.shows("Task t1") })

	// The command, decided by its graph's draft, changes the record for real
	f.ok("draft.tasks.task.reassign", "t1", map[string]any{"assignee": "ada"})
	if r := f.record("t1"); r["assignee"] != "ada" {
		t.Fatalf("after the draft's command: %+v", r)
	}
	f.ok("bp.import", "cmd-tasks-task-reassign", map[string]any{"graph": reassign("!")})
	eventually(t, "the graph's new draft deciding", func() bool {
		f.ok("draft.tasks.task.reassign", "t1", map[string]any{"assignee": "bob"})
		return f.record("t1")["assignee"] == "bob!"
	})
	// The installed names are not there: nothing is installed
	if res := f.do("tasks.task.reassign", "t1", map[string]any{"assignee": "eve"}); res.Common.Code < 300 {
		t.Error("the app's installed command answered without an install")
	}
	// The query, answered by its graph's draft
	var q map[string]any
	f.once("draft.tasks.myTasks", map[string]any{}, &q)
	if q["version"] != float64(1) {
		t.Errorf("draft query: %+v", q)
	}
	f.ok("bp.import", "qry-tasks-myTasks", map[string]any{"graph": myTasks(2)})
	eventually(t, "the query's new draft answering", func() bool {
		f.once("draft.tasks.myTasks", map[string]any{}, &q)
		return q["version"] == float64(2)
	})
	// A command the draft no longer names is not one
	f.ok("app.removeCommand", "tasks", map[string]any{"type": "task.reassign"})
	eventually(t, "the removed command gone", func() bool {
		return f.do("draft.tasks.task.reassign", "t1", map[string]any{"assignee": "x"}).Common.Code == 404
	})

	// No app may take the id draft
	f.refused("app.create", "draft", map[string]any{"name": "Draft"}, "manifest.id", "kept for apps' draft runs")
}
