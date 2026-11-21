package hello

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// The task list (forge 03i-TaskList, 04d-TaskListPrefabs): the entity type
// Task and the prefabs Task row and Task list, which Hello seeds in each
// tenant the first time the list opens, and the page, which is Task list.
// The prefabs are made by prefab.* commands alone (prefabs.go); their
// handlers, the toggle and clear-done commands and the open count are
// graphs (graphs/*task*.json). Its rows repeat Task row over the
// records.find query, with the filter and sort bound to the list's state.

// TaskEntity is the task list's entity type.
const TaskEntity = "task"

// taskType is Task as Hello seeds it.
var taskType = map[string]any{
	"name": "Task", "owner": "app:" + AppID, "titleField": "title",
	"description": "Something to do in the Hello app's shared task list, with a due date and an assignee",
	"fields": []map[string]any{
		{"id": "title", "name": "Title", "type": "text", "required": true},
		{"id": "done", "name": "Done", "type": "bool", "default": false},
		{"id": "due", "name": "Due", "type": "date"},
		{"id": "assignee", "name": "Assignee", "type": "relation", "relation": map[string]any{"entity": "user"}},
	},
}

// taskIndexes keep sorting by due quick, with or without the done filter.
var taskIndexes = []map[string]any{
	{"id": "by_due", "fields": []string{"due"}},
	{"id": "by_done_due", "fields": []string{"done", "due"}},
}

var (
	seedMu sync.Mutex
	seeded sync.Map // tenant → true once Task has a published version
)

// seedTasks defines and publishes Task in the tenant when it has no
// published version, as the tenant's service principal: the app's data
// is the app's, whoever opens it first (03i question 1). The Data
// Designer edits it like any other type afterwards.
func seedTasks(h *ui.Handle) error {
	tenant := h.Principal.TenantID
	if _, ok := seeded.Load(tenant); ok {
		return nil
	}
	seedMu.Lock()
	defer seedMu.Unlock()
	user := h.Principal
	h.Principal = features.ServicePrincipal(tenant)
	defer func() { h.Principal = user }()

	published := func() bool {
		_, fail := h.Query("entity.describe", map[string]any{"id": TaskEntity, "published": true})
		return fail == nil
	}
	if published() {
		seeded.Store(tenant, true)
		return nil
	}
	if problems, fail := h.Command("entity.create", TaskEntity, taskType); fail == nil {
		for _, x := range taskIndexes {
			if problems, fail := h.Command("entity.addIndex", TaskEntity, x); fail != nil {
				return refused("index "+x["id"].(string), problems, fail)
			}
		}
	} else if _, draft := h.Query("entity.describe", map[string]any{"id": TaskEntity}); draft != nil {
		// Not made, and no draft to publish either
		return refused("Task", problems, fail)
	}
	if err := publishDraft(h, "Task", "entity", TaskEntity); err != nil {
		return err
	}
	seeded.Store(tenant, true)
	return nil
}

// publishDraft publishes the draft of an entity type or prefab (kind) at
// the rev its projection shows, and waits until the published version can
// be read, as the page's queries read it.
func publishDraft(h *ui.Handle, what, kind, id string) error {
	published := func() bool {
		_, fail := h.Query(kind+".describe", map[string]any{"id": id, "published": true})
		return fail == nil
	}
	// The draft is read from its projection, which may not show the last
	// command yet (nor, just made, the draft): a stale rev is refused, and
	// both are read again
	for try := 0; ; try++ {
		data, fail := h.Query(kind+".describe", map[string]any{"id": id})
		if fail != nil {
			if try == 40 {
				return fmt.Errorf("%s cannot be read: %s", what, fail.Message)
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		raw, _ := json.Marshal(data)
		var d struct{ Rev int }
		_ = json.Unmarshal(raw, &d)
		problems, fail := h.Command(kind+".publish", id, map[string]any{"rev": d.Rev})
		if fail == nil || published() {
			break
		}
		if try == 20 || len(problems) == 0 || problems[0].Path != "args.rev" {
			return refused(what+"'s publication", problems, fail)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for try := 0; !published(); try++ {
		if try == 40 {
			return fmt.Errorf("%s was published but cannot be read yet", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func refused(what string, problems any, fail *features.ServiceResponse) error {
	raw, _ := json.Marshal(problems)
	return fmt.Errorf("%s could not be made: %s %s", what, fail.Message, raw)
}

func contributeTasks(reg *extensions.Registry) {
	page := ui.PrefabPage(TaskListPrefab, "", nil)
	page.Descriptor = extensions.Descriptor{ID: AppID + "/tasks", Source: "hello"}
	page.App, page.Title, page.Action = AppID, "Tasks", "hello.read"
	page.Description = "The Hello app's shared task list, the Task list prefab: add tasks, tick them done, rename them, show all, open or done ones, and sort them, live for everyone who has it open."
	page.Setup = func(h *ui.Handle) error {
		if err := seedTasks(h); err != nil {
			return err
		}
		return seedPrefabs(h)
	}
	page.Subject = func(json.RawMessage) ui.Subject {
		return ui.Subject{Type: "hello.tasks", ID: "main", Label: "Hello tasks"}
	}
	ui.Pages.Register(reg, page)
	ui.EditorKinds.Register(reg, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(AppID, "tasks"), Source: "hello"}, App: AppID, Kind: "tasks", Title: "Hello tasks", Icon: "checklist", Page: AppID + "/tasks"})
}
