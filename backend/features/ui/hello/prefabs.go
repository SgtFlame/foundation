package hello

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Task row and Task list (forge 04d-TaskListPrefabs), made only by
// prefab.* commands: Hello sends them as the tenant's service principal
// the first time a tenant opens the task list (04d question 1), and the
// Prefab Editor edits them afterwards. Their handlers are Hello's graphs.

// The prefabs' ids.
const (
	TaskRowPrefab  = "taskRow"
	TaskListPrefab = "taskList"
)

// op is one prefab.* command sent to a prefab.
type op struct {
	command string
	args    map[string]any
}

type obj = map[string]any

func insert(parent string, node obj) op {
	args := obj{"node": node}
	if parent != "" {
		args["parentPath"] = parent
	}
	return op{"prefab.insertNode", args}
}

func attach(name, graph string) op {
	return op{"prefab.attachGraph", obj{"name": name, "graph": graph}}
}

// taskRowOps build Task row: a checkbox bound to the task's done, its
// title in a text field, a due chip and the assignee when there are any,
// and the actions slot. It raises toggled and renamed.
var taskRowOps = []op{
	{"prefab.create", obj{"name": "Task row", "owner": "app:" + AppID,
		"description": "One task of the Hello task list: a checkbox for done, its title to edit, its due date as a chip, its assignee, and room for actions"}},
	{"prefab.setInterface", obj{
		"props": obj{"task": obj{"type": "record", "entity": TaskEntity, "required": true, "description": "The task shown"}},
		"events": obj{
			"toggled": obj{"description": "The checkbox was clicked", "payload": obj{"id": "string", "done": "bool"}},
			"renamed": obj{"description": "A new title was typed", "payload": obj{"id": "string", "title": "string"}},
		},
		"slots": obj{"actions": obj{"description": "Buttons at the row's end"}},
	}},
	attach("onToggle", "taskRow-onToggle"),
	attach("onRename", "taskRow-onRename"),
	insert("", obj{"type": "mui.stack", "key": "root", "props": obj{"direction": "row", "spacing": 1, "alignItems": "center"}}),
	insert("root", obj{"type": "mui.checkbox", "key": "check", "bind": obj{"checked": "props.task.done"},
		"on": obj{"change": obj{"handler": "onToggle"}}}),
	insert("root", obj{"type": "mui.textField", "key": "title", "props": obj{"placeholder": "Title"}, "bind": obj{"value": "props.task.title"},
		"on": obj{"blur": obj{"handler": "onRename", "payload": obj{"value": "event.value"}}, "submit": obj{"handler": "onRename", "payload": obj{"value": "event.value"}}}}),
	insert("root", obj{"type": "when", "key": "hasDue", "props": obj{"test": "props.task.due"}, "slots": obj{"then": []any{
		obj{"type": "mui.chip", "key": "due", "props": obj{"label": "", "color": "primary", "variant": "outlined"}, "bind": obj{"label": "props.task.due"}}}}}),
	insert("root", obj{"type": "when", "key": "hasAssignee", "props": obj{"test": "=props.task._expanded?.assignee != nil"}, "slots": obj{"then": []any{
		obj{"type": "mui.text", "key": "assignee", "props": obj{"variant": "body2", "sx": obj{"color": "text.secondary"}},
			"bind": obj{"text": `=props.task._expanded.assignee.displayName ?? ""`}}}}}),
	insert("root", obj{"type": "slot", "key": "actions", "props": obj{"name": "actions"}}),
	{"prefab.setPreview", obj{"props": obj{"task": obj{"_id": "preview", "title": "Write the plan", "done": false, "due": "2026-11-15"}}}},
}

// taskListOps build Task list: a heading with the open count, the add
// form, the show and sort controls and Clear done, and a Task row per task
// of its records.find query, whose where and sort follow its state.
var taskListOps = func() []op {
	addPayload := obj{"title": "local.title", "due": "local.due", "mine": "local.mine"}
	return []op{
		{"prefab.create", obj{"name": "Task list", "owner": "app:" + AppID,
			"description": "The Hello app's shared task list: add tasks, tick them done, rename them, show all, open or done ones, sort them, and clear the done ones"}},
		{"prefab.setInterface", obj{
			"props": obj{"heading": obj{"type": "string", "default": "Tasks", "description": "The list's heading"}},
			"state": obj{
				"show":      obj{"type": "enum", "options": []string{"all", "open", "done"}, "default": "all", "description": "Which tasks are shown"},
				"where":     obj{"type": "json", "default": obj{}, "description": "The tasks query's where, from show"},
				"sort":      obj{"type": "json", "default": []any{obj{"field": "due"}}, "description": "The tasks query's sort"},
				"sortShown": obj{"type": "json", "default": obj{"field": "due", "direction": "asc"}, "description": "The sort as the controls show it: {field, direction}"},
			},
			"queries": obj{
				"tasks": obj{"query": "records.find", "params": obj{"entity": TaskEntity, "expand": []string{"assignee"}},
					"bind": obj{"where": "state.where", "sort": "state.sort"}, "description": "The tasks shown, live"},
				"open": obj{"query": "hello.openTasks", "description": "How many tasks are open"},
			},
		}},
		attach("onAdd", "hello-onAddTask"),
		attach("onShow", "hello-onFilterTasks"),
		attach("onSort", "hello-onSortTasks"),
		attach("onClearDone", "hello-onClearDone"),
		attach("onToggled", "hello-onTaskToggled"),
		attach("onRenamed", "hello-onTaskRenamed"),
		insert("", obj{"type": "mui.stack", "key": "root", "props": obj{"spacing": 2, "sx": obj{"p": 2}}}),
		insert("root", obj{"type": "mui.stack", "key": "head", "props": obj{"direction": "row", "spacing": 2, "alignItems": "center"}}),
		insert("root/head", obj{"type": "mui.heading", "key": "title", "props": obj{"text": "Tasks", "level": 4}, "bind": obj{"text": "props.heading"}}),
		insert("root/head", obj{"type": "mui.chip", "key": "open", "props": obj{"label": ""}, "bind": obj{"label": "query:open.label"}}),
		insert("root", obj{"type": "mui.stack", "key": "add", "props": obj{"direction": "row", "spacing": 1, "alignItems": "center", "wrap": true}}),
		insert("root/add", obj{"type": "mui.textField", "key": "newTitle", "props": obj{"label": "New task"}, "bind": obj{"value": "local.title"},
			"on": obj{"submit": obj{"handler": "onAdd", "payload": addPayload}}}),
		insert("root/add", obj{"type": "mui.date", "key": "newDue", "props": obj{"label": "Due", "mode": "date"}, "bind": obj{"value": "local.due"}}),
		insert("root/add", obj{"type": "mui.switch", "key": "newMine", "props": obj{"label": "Assign to me"}, "bind": obj{"checked": "local.mine"}}),
		insert("root/add", obj{"type": "mui.button", "key": "addTask", "props": obj{"label": "Add", "variant": "contained", "icon": "add"},
			"on": obj{"click": obj{"handler": "onAdd", "payload": addPayload}}}),
		insert("root", obj{"type": "mui.stack", "key": "tools", "props": obj{"direction": "row", "spacing": 1, "alignItems": "center", "wrap": true}}),
		insert("root/tools", obj{"type": "mui.select", "key": "show", "props": obj{"label": "Show", "options": []obj{
			{"value": "all", "label": "All"}, {"value": "open", "label": "Open"}, {"value": "done", "label": "Done"}}},
			"bind": obj{"value": "state.show"}, "on": obj{"change": obj{"handler": "onShow", "payload": obj{"value": "event.value"}}}}),
		// Sort by a field, then flip the direction (04d question 3)
		insert("root/tools", obj{"type": "mui.select", "key": "sortBy", "props": obj{"label": "Sort by", "options": []obj{
			{"value": "due", "label": "Due"}, {"value": "title", "label": "Title"}, {"value": "_createdAt", "label": "Created"}}},
			"bind": obj{"value": "state.sortShown.field"},
			"on":   obj{"change": obj{"handler": "onSort", "payload": obj{"field": "event.value", "direction": "=state.sortShown.direction"}}}}),
		insert("root/tools", obj{"type": "mui.button", "key": "direction", "props": obj{"label": "Ascending", "variant": "outlined"},
			"bind": obj{"label": `=state.sortShown.direction == "desc" ? "Descending" : "Ascending"`,
				"icon": `=state.sortShown.direction == "desc" ? "arrow_downward" : "arrow_upward"`},
			"on": obj{"click": obj{"handler": "onSort", "payload": obj{"field": "=state.sortShown.field",
				"direction": `=state.sortShown.direction == "desc" ? "asc" : "desc"`}}}}),
		insert("root/tools", obj{"type": "mui.button", "key": "clearDone", "props": obj{"label": "Clear done", "variant": "outlined", "icon": "delete"},
			"on": obj{"click": obj{"handler": "onClearDone"}}}),
		insert("root", obj{"type": "mui.stack", "key": "list", "props": obj{"spacing": 1}}),
		insert("root/list", obj{"type": "repeat", "key": "rows", "props": obj{"each": "query:tasks", "as": "task", "key": "task._id"}}),
		insert("root/list/rows", obj{"type": "prefab." + TaskRowPrefab, "key": "row", "bind": obj{"task": "task"},
			"on": obj{"toggled": obj{"handler": "onToggled"}, "renamed": obj{"handler": "onRenamed"}}}),
		insert("root/list", obj{"type": "when", "key": "none", "props": obj{"test": "=len(queries.tasks?.records ?? []) == 0"}, "slots": obj{"then": []any{
			obj{"type": "mui.empty", "key": "empty", "props": obj{"icon": "checklist", "title": "No tasks", "text": "Add one above, or show all tasks"}}}}}),
	}
}()

var prefabsSeeded sync.Map // tenant → true once both prefabs are published

// seedPrefabs makes and publishes Task row, then Task list, by their
// prefab.* commands, when the tenant has no published version of them.
// A draft left unpublished is published as it is.
func seedPrefabs(h *ui.Handle) error {
	tenant := h.Principal.TenantID
	if _, ok := prefabsSeeded.Load(tenant); ok {
		return nil
	}
	seedMu.Lock()
	defer seedMu.Unlock()
	user := h.Principal
	h.Principal = features.ServicePrincipal(tenant)
	defer func() { h.Principal = user }()

	for _, p := range []struct {
		id, name string
		ops      []op
	}{{TaskRowPrefab, "Task row", taskRowOps}, {TaskListPrefab, "Task list", taskListOps}} {
		if _, fail := h.Query("prefab.describe", obj{"id": p.id, "published": true}); fail == nil {
			continue
		}
		if _, fail := h.Query("prefab.describe", obj{"id": p.id}); fail != nil {
			for _, o := range p.ops {
				if problems, fail := h.Command(o.command, p.id, o.args); fail != nil {
					return refused(p.name+" ("+o.command+")", problems, fail)
				}
			}
		}
		if err := publishDraft(h, p.name, "prefab", p.id); err != nil {
			return err
		}
	}
	prefabsSeeded.Store(tenant, true)
	return nil
}
