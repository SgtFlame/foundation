package hello

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// The items the main page lists; each opens as its own top-level tab.
var items = []map[string]any{
	{"id": "1", "primary": "First item", "secondary": "Opens as its own tab"},
	{"id": "2", "primary": "Second item", "secondary": "Opens as its own tab"},
	{"id": "3", "primary": "Third item", "secondary": "Opens as its own tab"},
}

func layout(selected string) map[string]any {
	sel := 0
	if selected == "items" {
		sel = 1
	}
	return map[string]any{"row": map[string]any{"type": "row", "children": []any{
		map[string]any{"type": "tabset", "selected": sel, "children": []any{
			map[string]any{"type": "tab", "id": "graph", "name": "Graph"},
			map[string]any{"type": "tab", "id": "items", "name": "Items"},
		}},
	}}}
}

func counterCard(key string) *ui.Node {
	return ui.N("mui.card", key).P("title", "Shared counter", "variant", "outlined").C(
		ui.N("mui.stack", "row").P("direction", "row", "spacing", 2, "alignItems", "center").C(
			ui.N("mui.heading", "value").P("level", 3).B("text", "query:counter.value"),
			ui.N("mui.button", "increment").P("label", "Increment", "variant", "contained", "icon", "add").E("click", "hello-onIncrement"),
		),
	)
}

func contributePages(reg *extensions.Registry) {
	counter := func(json.RawMessage) map[string]ui.QueryRef {
		return map[string]ui.QueryRef{"counter": {Query: "hello.counter", Params: map[string]string{"id": "main"}}}
	}
	ui.Pages.Register(reg, ui.Page{
		Descriptor: extensions.Descriptor{ID: AppID + "/main", Source: "hello"}, App: AppID, Title: "Hello", Action: "hello.read",
		Description: "The Hello app: a shared counter, a greeting kept in view state, and docked sub-tabs with a shared graph and items that open as their own tabs.",
		Queries: func(params json.RawMessage) map[string]ui.QueryRef {
			q := counter(params)
			q["graph"] = ui.QueryRef{Query: "hello.graph", Params: map[string]string{"id": "main"}}
			return q
		},
		Subject: func(json.RawMessage) ui.Subject { return ui.Subject{Type: "app", ID: AppID, Label: "Hello"} },
		OnOpen: func(h *ui.Handle) error {
			h.Set("tab", "graph")
			h.Set("greeting", "Type your name and press Greet.")
			return nil
		},
		Render: func(r *ui.Render) *ui.Node {
			return ui.N("mui.stack", "root").P("spacing", 2, "sx", map[string]any{"p": 2, "height": "100%", "boxSizing": "border-box"}).C(
				ui.N("mui.stack", "top").P("direction", "row", "spacing", 2, "wrap", true).C(
					counterCard("counter"),
					ui.N("mui.card", "greet").P("title", "Greeting", "subtitle", "Typing stays in the browser; Greet sends it", "variant", "outlined").C(
						ui.N("mui.stack", "form").P("direction", "row", "spacing", 1, "alignItems", "center").C(
							ui.N("mui.textField", "name").P("label", "Your name").B("value", "local.name").E("submit", "hello-onGreet", "name", "local.name"),
							ui.N("mui.button", "greet").P("label", "Greet").E("click", "hello-onGreet", "name", "local.name"),
						),
						r.Region("greeting", func(r *ui.Render) *ui.Node {
							text, _ := r.State("greeting").(string)
							return ui.N("mui.text", "greeting").P("text", text, "sx", map[string]any{"mt": 1})
						}),
					),
					ui.N("mui.card", "tasks").P("title", "Task list", "subtitle", "Shared tasks, records of the Task entity type", "variant", "outlined").C(
						ui.N("mui.button", "openTasks").P("label", "Open task list", "variant", "contained", "icon", "checklist").E("click", "hello-onOpenTasks"),
					),
				),
				r.Region("tabs", func(r *ui.Render) *ui.Node {
					tab, _ := r.State("tab").(string)
					return ui.N("layout.flex", "tabs").P("layout", layout(tab), "sx", map[string]any{"flex": 1, "minHeight": 320}).E("select", "hello-onTab", "id", "event.id").C(
						ui.N("graph.canvas", "graph").P("mode", "free").B("nodes", "query:graph.nodes").B("edges", "query:graph.edges").
							E("connect", "hello-onConnect", "source", "event.source", "sourceHandle", "event.sourceHandle", "target", "event.target", "targetHandle", "event.targetHandle").
							E("disconnect", "hello-onDisconnect", "id", "event.id").
							E("delete", "hello-onDelete", "edges", "event.edges").
							E("move", "hello-onMove", "id", "event.id", "position", "event.position").
							E("select", "hello-onSelect", "nodes", "event.nodes"),
						ui.N("mui.stack", "items").P("spacing", 1, "sx", map[string]any{"p": 2}).C(
							ui.N("mui.text", "hint").P("text", "Open an item as its own top-level tab; it reopens after a reload.", "variant", "body2"),
							ui.N("mui.list", "list").P("items", items).E("select", "hello-onOpenItem", "id", "event.id"),
						),
					)
				}),
			)
		},
	})

	ui.Pages.Register(reg, ui.Page{
		Descriptor: extensions.Descriptor{ID: AppID + "/item", Source: "hello"}, App: AppID, Title: "Hello item", Action: "hello.read",
		Description: "One item of the Hello app, in its own top-level tab, with the shared counter.",
		Queries:     counter,
		Subject: func(params json.RawMessage) ui.Subject {
			id, _ := idParam(params)
			return ui.Subject{Type: "hello.item", ID: id, Label: "Hello item " + id}
		},
		Render: func(r *ui.Render) *ui.Node {
			id, _ := idParam(r.Params())
			return ui.N("mui.stack", "root").P("spacing", 2, "sx", map[string]any{"p": 2}).C(
				ui.N("mui.heading", "title").P("text", "Item "+id, "level", 4),
				ui.N("mui.text", "about").P("text", "This tab is the Hello app's item editor kind. The counter below is the same shared counter as on the app's own tab."),
				counterCard("counter"),
			)
		},
	})

	contributeTasks(reg)

	ui.EditorKinds.Register(reg, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(AppID, "main"), Source: "hello"}, App: AppID, Kind: "main", Title: "Hello", Icon: "waving_hand", Page: AppID + "/main"})
	ui.EditorKinds.Register(reg, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(AppID, "item"), Source: "hello"}, App: AppID, Kind: "item", Title: "Hello item", Icon: "description", Page: AppID + "/item"})
}
