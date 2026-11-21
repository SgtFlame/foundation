package prefabs_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/tools"
)

// TestTheChatBuildsAContactCard is forge 04f's chat acceptance as the
// commands a model sends for "a card that shows a contact's name, email and
// an Edit button that emits edit", each through its tool: a Contact entity
// type, the prefab and its interface, its nodes, a ui.handler graph raising
// edit, preview data, a preview with no problems, and publishing.
func TestTheChatBuildsAContactCard(t *testing.T) {
	f := setup(t)
	f.base()
	run := f.tool
	rev := func(content map[string]any) int {
		t.Helper()
		raw, _ := json.Marshal(content["report"])
		var r struct{ Rev int }
		if json.Unmarshal(raw, &r) != nil || r.Rev == 0 {
			t.Fatalf("no rev in %s", raw)
		}
		return r.Rev
	}

	// Find what there is
	run("prefab.list", map[string]any{})
	run("prefab.listTypes", map[string]any{})

	// The contact entity type
	res := run("entity.create", map[string]any{"target": "contact", "args": map[string]any{"name": "Contact", "fields": []map[string]any{
		{"name": "name", "type": "text"}, {"name": "email", "type": "text"}}}})
	run("entity.publish", map[string]any{"target": "contact", "args": map[string]any{"rev": rev(res.Content)}})

	// The prefab, its interface and its tree
	res = run("prefab.create", map[string]any{"args": map[string]any{"name": "Contact card", "description": "A contact's name and email, with an Edit button"}})
	if res.Content["target"] != "contactCard" {
		t.Fatalf("prefab.create: %v", res.Content)
	}
	card := func(command string, a map[string]any) map[string]any {
		t.Helper()
		return run(command, map[string]any{"target": "contactCard", "args": a}).Content
	}
	card("prefab.setInterface", map[string]any{
		"props":  map[string]any{"contact": map[string]any{"type": "record", "entity": "contact", "required": true, "description": "The contact to show"}},
		"events": map[string]any{"edit": map[string]any{"description": "Edit was clicked", "payload": map[string]any{"id": "string"}}},
	})
	card("prefab.insertNode", map[string]any{"node": map[string]any{"type": "mui.card", "key": "root"}})
	// A command's args put beside args are refused, saying where they go
	for _, tool := range tools.Tools.All(f.reg) {
		if tool.ID == "prefab.insertNode" {
			raw, _ := json.Marshal(map[string]any{"target": "contactCard", "parentPath": "root", "args": map[string]any{"node": map[string]any{"type": "mui.divider"}}})
			if _, err := tool.Run(tools.Call{ServiceArgs: args(), Name: tool.ID, Args: raw}); err == nil || !strings.Contains(err.Error(), "parentPath belong inside args") {
				t.Errorf("misplaced args: %v", err)
			}
		}
	}
	card("prefab.insertNode", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.text", "key": "name", "props": map[string]any{"variant": "subtitle1"}, "bind": map[string]any{"text": "props.contact.name"}}})
	card("prefab.insertNode", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.text", "key": "email", "bind": map[string]any{"text": "props.contact.email"}}})

	// The handler that raises edit with the contact's id
	g := func(command string, a map[string]any) map[string]any {
		t.Helper()
		return run(command, map[string]any{"target": "contactCard-onEdit", "args": a}).Content
	}
	g("bp.create", map[string]any{"kind": "ui.handler", "name": "Contact card: Edit clicked", "owner": "prefab:contactCard"})
	g("bp.addNode", map[string]any{"id": "id", "type": "ui.getProp", "config": map[string]any{"path": "contact._id"}})
	g("bp.addNode", map[string]any{"id": "raise", "type": "ui.raiseEvent", "config": map[string]any{"event": "edit", "inputs": []any{map[string]any{"id": "id", "type": "string"}}}})
	g("bp.connect", map[string]any{"from": map[string]any{"node": "event", "pin": "then"}, "to": map[string]any{"node": "raise", "pin": "then"}})
	last := g("bp.connect", map[string]any{"from": map[string]any{"node": "id", "pin": "value"}, "to": map[string]any{"node": "raise", "pin": "id"}})
	g("bp.publish", map[string]any{"rev": rev(last)})

	card("prefab.attachGraph", map[string]any{"name": "onEdit", "graph": "contactCard-onEdit"})
	card("prefab.insertNode", map[string]any{"parentPath": "root", "slot": "actions", "node": map[string]any{"type": "mui.button", "key": "edit",
		"props": map[string]any{"label": "Edit"}, "on": map[string]any{"click": map[string]any{"handler": "onEdit"}}}})
	last = card("prefab.setPreview", map[string]any{"props": map[string]any{"contact": map[string]any{"_id": "c1", "name": "Ada Lovelace", "email": "ada@example.com"}}})

	// The draft's preview has no problems, then it is published
	res = run("prefab.preview", map[string]any{"prefab": "contactCard", "draft": true})
	tree, _ := json.Marshal(res.Content["tree"])
	if res.Content["valid"] != true || !strings.Contains(string(tree), `"Ada Lovelace"`) || !strings.Contains(string(tree), `"ada@example.com"`) || !strings.Contains(string(tree), `"label":"Edit"`) {
		t.Fatalf("the draft's preview: %v %s", res.Content["problems"], tree)
	}
	card("prefab.publish", map[string]any{"rev": rev(last)})
	res = run("prefab.preview", map[string]any{"prefab": "contactCard"})
	if res.Content["valid"] != true {
		t.Errorf("the published preview: %v", res.Content["problems"])
	}
	if v := f.describe("contactCard"); v.Published != 1 || v.Prefab.Events["edit"].Payload["id"] != "string" {
		t.Errorf("published: %+v", v)
	}
}
