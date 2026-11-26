package diff

import (
	"encoding/json"
	"testing"
)

func parse(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func want(t *testing.T, got []Change, expect ...Change) {
	t.Helper()
	if len(got) != len(expect) {
		t.Fatalf("got %d changes, want %d: %+v", len(got), len(expect), got)
	}
	for i, e := range expect {
		if got[i].Path != e.Path || got[i].Kind != e.Kind {
			t.Errorf("change %d: got %s %s, want %s %s", i, got[i].Kind, got[i].Path, e.Kind, e.Path)
		}
	}
}

// A field renamed keeps its id, so it is changed, not removed and added.
func TestEntityFieldRenamed(t *testing.T) {
	a := parse(t, `{"id":"refund","name":"Refund","fields":{"limit":{"id":"limit","name":"Limit","type":"number"}}}`)
	b := parse(t, `{"id":"refund","name":"Refund","fields":{"limit":{"id":"limit","name":"Cap","type":"number"},"note":{"id":"note","name":"Note","type":"text"}}}`)
	got := Definitions("entity", "refund", a, b)
	want(t, got,
		Change{Path: "entity:refund.fields.limit.name", Kind: Changed},
		Change{Path: "entity:refund.fields.note", Kind: Added})
	if got[0].Before != "Limit" || got[0].After != "Cap" {
		t.Errorf("rename: %v -> %v", got[0].Before, got[0].After)
	}
}

// A graph node only repositioned is moved; edges are known by their ends.
func TestGraphNodeMoved(t *testing.T) {
	a := parse(t, `{"id":"g","nodes":[{"id":"n1","type":"event","position":{"x":0,"y":0}},{"id":"n2","type":"math","position":{"x":100,"y":0}}],
		"edges":[{"from":{"node":"n1","pin":"exec"},"to":{"node":"n2","pin":"exec"}}]}`)
	b := parse(t, `{"id":"g","nodes":[{"id":"n1","type":"event","position":{"x":0,"y":50}},{"id":"n2","type":"math","position":{"x":100,"y":0},"config":{"op":"add"}},{"id":"n3","type":"return","position":{"x":200,"y":0}}],
		"edges":[{"from":{"node":"n2","pin":"exec"},"to":{"node":"n3","pin":"exec"}}]}`)
	want(t, Definitions("graph", "g", a, b),
		Change{Path: "graph:g.nodes.n1", Kind: Moved},
		Change{Path: "graph:g.nodes.n2.config", Kind: Added},
		Change{Path: "graph:g.nodes.n3", Kind: Added},
		Change{Path: "graph:g.edges[n1.exec->n2.exec]", Kind: Removed},
		Change{Path: "graph:g.edges[n2.exec->n3.exec]", Kind: Added})
}

// A subtree moved under another parent is one move; a sibling inserted
// moves nothing; a node removed with its parent is one removal.
func TestPrefabSubtreeMoved(t *testing.T) {
	a := parse(t, `{"id":"row","name":"Row","template":{"key":"root","type":"mui.stack","children":[
		{"key":"title","type":"mui.typography"},
		{"key":"actions","type":"mui.stack","children":[{"key":"edit","type":"mui.button"},{"key":"del","type":"mui.button"}]},
		{"key":"footer","type":"mui.box","children":[{"key":"hint","type":"mui.typography"}]}]}}`)
	b := parse(t, `{"id":"row","name":"Row","template":{"key":"root","type":"mui.stack","children":[
		{"key":"badge","type":"mui.chip"},
		{"key":"title","type":"mui.typography","props":{"variant":"h6"}},
		{"key":"side","type":"mui.box","children":[
			{"key":"actions","type":"mui.stack","children":[{"key":"edit","type":"mui.button"},{"key":"del","type":"mui.button"}]}]}]}}`)
	want(t, Definitions("prefab", "row", a, b),
		Change{Path: "prefab:row.tree.root/footer", Kind: Removed},
		Change{Path: "prefab:row.tree.root/badge", Kind: Added},
		Change{Path: "prefab:row.tree.root/side", Kind: Added},
		Change{Path: "prefab:row.tree.root/side/actions", Kind: Moved},
		Change{Path: "prefab:row.tree.root/title.props", Kind: Added})
}

// A manifest's keyed lists are diffed by key.
func TestManifestPages(t *testing.T) {
	a := parse(t, `{"id":"shop","name":"Shop","pages":[{"route":"/orders","prefab":"orders","title":"Orders"},{"route":"/old","prefab":"old"}]}`)
	b := parse(t, `{"id":"shop","name":"Shop","pages":[{"route":"/orders","prefab":"orders","title":"All orders"},{"route":"/new","prefab":"new"}]}`)
	want(t, Definitions("app", "shop", a, b),
		Change{Path: "app:shop.pages[/old]", Kind: Removed},
		Change{Path: "app:shop.pages[/orders].title", Kind: Changed},
		Change{Path: "app:shop.pages[/new]", Kind: Added})
}

// A part made or removed (the branch tombstone) is one change.
func TestPartAddedRemoved(t *testing.T) {
	draft := json.RawMessage(`{"prefab":{"id":"x","name":"X"},"rev":3}`)
	want(t, Part("prefab", "x", nil, draft), Change{Path: "prefab:x", Kind: Added})
	want(t, Part("prefab", "x", draft, json.RawMessage(`{"$removed":true}`)), Change{Path: "prefab:x", Kind: Removed})
	want(t, Part("prefab", "x", draft, draft))
}

// Names, titles and descriptions alone are trivial; anything else is not.
func TestTrivial(t *testing.T) {
	a := parse(t, `{"id":"row","name":"Row","description":"A row","template":{"key":"root","type":"mui.box"}}`)
	b := parse(t, `{"id":"row","name":"Order row","description":"One order","template":{"key":"root","type":"mui.box"}}`)
	if !Trivial(Definitions("prefab", "row", a, b)) {
		t.Error("a renamed prefab is not trivial")
	}
	c := parse(t, `{"id":"row","name":"Row","template":{"key":"root","type":"mui.stack"}}`)
	if Trivial(Definitions("prefab", "row", a, c)) {
		t.Error("a changed component is trivial")
	}
}
