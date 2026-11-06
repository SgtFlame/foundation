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

// The task list (forge 03i-TaskList): the entity type Task, which Hello
// seeds in each tenant the first time the list opens, and a page whose
// handlers, toggle and clear-done commands and open count are graphs
// (graphs/*task*.json). Its rows are the records.find query, with the
// filter and sort bound to view state.

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
	// The draft's rev is read from its projection, which may not show the
	// last command yet: a stale rev is refused, and read again
	for try := 0; ; try++ {
		data, fail := h.Query("entity.describe", map[string]any{"id": TaskEntity})
		if fail != nil {
			return fmt.Errorf("Task cannot be read: %s", fail.Message)
		}
		raw, _ := json.Marshal(data)
		var d struct{ Rev int }
		_ = json.Unmarshal(raw, &d)
		problems, fail := h.Command("entity.publish", TaskEntity, map[string]any{"rev": d.Rev})
		if fail == nil || published() {
			break
		}
		if try == 20 || len(problems) == 0 || problems[0].Path != "args.rev" {
			return refused("Task's publication", problems, fail)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The page's queries read the published version: wait until it shows
	for try := 0; !published(); try++ {
		if try == 40 {
			return fmt.Errorf("Task was published but cannot be read yet")
		}
		time.Sleep(50 * time.Millisecond)
	}
	seeded.Store(tenant, true)
	return nil
}

func refused(what string, problems any, fail *features.ServiceResponse) error {
	raw, _ := json.Marshal(problems)
	return fmt.Errorf("%s could not be made: %s %s", what, fail.Message, raw)
}

// taskColumns are the table's columns: Task's visible published fields,
// so a field added and published shows as a column at once.
func taskColumns(entity any) []map[string]any {
	e, _ := entity.(map[string]any)
	fields, _ := e["fields"].([]any)
	var cols []map[string]any
	for _, x := range fields {
		f, _ := x.(map[string]any)
		if hidden, _ := f["hidden"].(bool); hidden {
			continue
		}
		id, _ := f["id"].(string)
		typ, _ := f["type"].(string)
		col := map[string]any{"field": id, "title": f["name"], "sortable": true}
		switch typ {
		case "bool":
			col["type"], col["editable"], col["width"] = "bool", true, 72
		case "date", "dateTime", "number":
			col["type"] = typ
		case "enum":
			var options []map[string]any
			opts, _ := f["options"].([]any)
			for _, o := range opts {
				om, _ := o.(map[string]any)
				options = append(options, map[string]any{"value": om["id"], "label": om["name"]})
			}
			col["type"], col["options"] = "enum", options
		case "relation":
			rel, _ := f["relation"].(map[string]any)
			if rel["entity"] == "user" {
				col["field"], col["sortField"] = "_expanded."+id+".displayName", id
			} else {
				col["sortable"] = false
			}
		case "multiEnum", "tags", "list", "json", "richText", "file":
			col["sortable"] = false
		}
		cols = append(cols, col)
	}
	return cols
}

func contributeTasks(reg *extensions.Registry) {
	ui.Pages.Register(reg, ui.Page{
		Descriptor: extensions.Descriptor{ID: AppID + "/tasks", Source: "hello"}, App: AppID, Title: "Tasks", Action: "hello.read",
		Description: "The Hello app's shared task list: add tasks, tick them done, show all, open or done ones, and sort by any column, live for everyone who has it open.",
		Setup:       seedTasks,
		Queries: func(json.RawMessage) map[string]ui.QueryRef {
			return map[string]ui.QueryRef{
				"type": {Query: "entity.describe", Params: map[string]any{"id": TaskEntity, "published": true}},
				"tasks": {Query: "records.find", Params: map[string]any{"entity": TaskEntity, "expand": []string{"assignee"}},
					Bind: map[string]string{"where": "state.where", "sort": "state.sort"}},
				"open": {Query: "hello.openTasks", Params: map[string]any{}},
			}
		},
		Subject: func(json.RawMessage) ui.Subject { return ui.Subject{Type: "hello.tasks", ID: "main", Label: "Hello tasks"} },
		OnOpen: func(h *ui.Handle) error {
			h.Set("show", "all")
			h.Set("where", map[string]any{})
			h.Set("sort", []any{map[string]any{"field": "due"}})
			h.Set("sortShown", map[string]any{"field": "due", "direction": "asc"})
			return nil
		},
		Render: func(r *ui.Render) *ui.Node {
			return ui.N("mui.stack", "root").P("spacing", 2, "sx", map[string]any{"p": 2}).C(
				ui.N("mui.stack", "head").P("direction", "row", "spacing", 2, "alignItems", "center").C(
					ui.N("mui.heading", "title").P("text", "Tasks", "level", 4),
					ui.N("mui.chip", "open").P("label", "").B("label", "query:open.label"),
				),
				ui.N("mui.stack", "add").P("direction", "row", "spacing", 1, "alignItems", "center", "wrap", true).C(
					ui.N("mui.textField", "newTitle").P("label", "New task").B("value", "local.title").
						E("submit", "hello-onAddTask", "title", "local.title", "due", "local.due", "mine", "local.mine"),
					ui.N("mui.date", "newDue").P("label", "Due", "mode", "date").B("value", "local.due"),
					ui.N("mui.switch", "newMine").P("label", "Assign to me").B("checked", "local.mine"),
					ui.N("mui.button", "addTask").P("label", "Add", "variant", "contained", "icon", "add").
						E("click", "hello-onAddTask", "title", "local.title", "due", "local.due", "mine", "local.mine"),
				),
				ui.N("mui.stack", "tools").P("direction", "row", "spacing", 1, "alignItems", "center").C(
					ui.N("mui.select", "show").P("label", "Show", "options", []map[string]any{
						{"value": "all", "label": "All"}, {"value": "open", "label": "Open"}, {"value": "done", "label": "Done"},
					}).B("value", "state.show").E("change", "hello-onFilterTasks", "value", "event.value"),
					ui.N("mui.button", "clearDone").P("label", "Clear done", "variant", "outlined", "icon", "delete").E("click", "hello-onClearDone"),
				),
				r.Region("list", func(r *ui.Render) *ui.Node {
					cols := taskColumns(r.Query("type", "entity"))
					if len(cols) == 0 {
						return ui.N("mui.empty", "list").P("title", "Task is not published yet")
					}
					return ui.N("mui.table", "list").P("columns", cols, "rows", []any{}, "rowKey", "_id", "height", 420).
						B("rows", "query:tasks.records").B("sort", "state.sortShown").
						E("sort", "hello-onSortTasks", "field", "event.field", "direction", "event.direction").
						E("edit", "hello-onEditTask", "key", "event.key", "field", "event.field", "value", "event.value")
				}),
			)
		},
	})
	ui.EditorKinds.Register(reg, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(AppID, "tasks"), Source: "hello"}, App: AppID, Kind: "tasks", Title: "Hello tasks", Icon: "checklist", Page: AppID + "/tasks"})
}
