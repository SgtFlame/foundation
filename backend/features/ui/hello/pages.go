package hello

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
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
			ui.N("mui.button", "increment").P("label", "Increment", "variant", "contained", "icon", "add").E("click", "increment"),
		),
	)
}

func contributePages(reg *extensions.Registry) {
	counter := func(json.RawMessage) map[string]ui.QueryRef {
		return map[string]ui.QueryRef{"counter": {Query: "hello.counter", Params: map[string]string{"id": "main"}}}
	}
	increment := func(h *ui.Handle) error {
		if problems, fail := h.Command("hello.increment", "main", map[string]int{"by": 1}); fail != nil {
			h.Toast("error", commandMessage(problems, fail.Message))
		}
		return nil
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
							ui.N("mui.textField", "name").P("label", "Your name").B("value", "local.name").E("submit", "greet", "name", "local.name"),
							ui.N("mui.button", "greet").P("label", "Greet").E("click", "greet", "name", "local.name"),
						),
						r.Region("greeting", func(r *ui.Render) *ui.Node {
							text, _ := r.State("greeting").(string)
							return ui.N("mui.text", "greeting").P("text", text, "sx", map[string]any{"mt": 1})
						}),
					),
				),
				r.Region("tabs", func(r *ui.Render) *ui.Node {
					tab, _ := r.State("tab").(string)
					return ui.N("layout.flex", "tabs").P("layout", layout(tab), "sx", map[string]any{"flex": 1, "minHeight": 320}).E("select", "tab", "id", "event.id").C(
						ui.N("graph.canvas", "graph").P("mode", "free").B("nodes", "query:graph.nodes").B("edges", "query:graph.edges").
							E("connect", "connect", "source", "event.source", "sourceHandle", "event.sourceHandle", "target", "event.target", "targetHandle", "event.targetHandle").
							E("disconnect", "disconnect", "id", "event.id").
							E("delete", "delete", "edges", "event.edges").
							E("move", "move", "id", "event.id", "position", "event.position").
							E("select", "select", "nodes", "event.nodes"),
						ui.N("mui.stack", "items").P("spacing", 1, "sx", map[string]any{"p": 2}).C(
							ui.N("mui.text", "hint").P("text", "Open an item as its own top-level tab; it reopens after a reload.", "variant", "body2"),
							ui.N("mui.list", "list").P("items", items).E("select", "openItem", "id", "event.id"),
						),
					)
				}),
			)
		},
		Handlers: map[string]func(h *ui.Handle) error{
			"increment": increment,
			"greet": func(h *ui.Handle) error {
				name := h.String("name")
				if name == "" {
					h.Toast("warning", "Type a name first.")
					return nil
				}
				h.Set("greeting", fmt.Sprintf("Hello, %s!", name))
				return nil
			},
			"tab": func(h *ui.Handle) error {
				if id := h.String("id"); id == "graph" || id == "items" {
					h.Set("tab", id)
				}
				return nil
			},
			"connect": func(h *ui.Handle) error {
				return graph(h, "hello.connect", map[string]any{"source": h.Payload["source"], "sourceHandle": h.Payload["sourceHandle"], "target": h.Payload["target"], "targetHandle": h.Payload["targetHandle"]})
			},
			"disconnect": func(h *ui.Handle) error { return graph(h, "hello.disconnect", map[string]any{"id": h.Payload["id"]}) },
			"delete": func(h *ui.Handle) error {
				edges, _ := h.Payload["edges"].([]any)
				for _, e := range edges {
					if err := graph(h, "hello.disconnect", map[string]any{"id": e}); err != nil {
						return err
					}
				}
				return nil
			},
			"move": func(h *ui.Handle) error {
				return graph(h, "hello.moveNode", map[string]any{"id": h.Payload["id"], "position": h.Payload["position"]})
			},
			"select": func(h *ui.Handle) error {
				h.Set("selected", h.Payload["nodes"])
				return nil
			},
			"openItem": func(h *ui.Handle) error {
				id := h.String("id")
				h.OpenEditor("item", id, "Hello item "+id, "An item of the Hello app", map[string]string{"id": id})
				return nil
			},
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
		Handlers: map[string]func(h *ui.Handle) error{"increment": increment},
	})

	ui.EditorKinds.Register(reg, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(AppID, "main"), Source: "hello"}, App: AppID, Kind: "main", Title: "Hello", Icon: "waving_hand", Page: AppID + "/main"})
	ui.EditorKinds.Register(reg, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(AppID, "item"), Source: "hello"}, App: AppID, Kind: "item", Title: "Hello item", Icon: "description", Page: AppID + "/item"})
}

func graph(h *ui.Handle, command string, args map[string]any) error {
	if problems, fail := h.Command(command, "main", args); fail != nil {
		h.Toast("error", commandMessage(problems, fail.Message))
	}
	return nil
}

func commandMessage(problems []live.Problem, fallback string) string {
	if len(problems) > 0 {
		return problems[0].Message
	}
	return fallback
}
