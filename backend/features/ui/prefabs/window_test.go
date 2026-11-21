package prefabs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Windowed collections (forge 07l): a virtualList renders only its window
// of records.find pages, moves it when the browser reports other rows,
// walks to a far one by skipping, and patches a changed row in place.

func (st *render) items(n int) []string {
	st.t.Helper()
	st.do("entity.create", "item", map[string]any{"name": "Item", "fields": []map[string]any{{"name": "title", "type": "text"}}})
	st.do("entity.publish", "item", map[string]int{"rev": st.rev(entities.DraftProjector, "entity.item", "item")})
	if _, err := st.p.WaitFor(context.Background(), 1, entities.PublishedProjector, "entity.item", st.seq["item"], 2*time.Second); err != nil {
		st.t.Fatal(err)
	}
	ids := make([]string, n)
	for i := range n {
		ids[i] = st.do("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"title": fmt.Sprintf("Item %03d", i)}}).Target
	}
	return ids
}

// follow applies the session's patches to tree until ok holds.
func (ss *session) follow(tree *ui.Node, what string, ok func(*ui.Node) bool) *ui.Node {
	ss.t.Helper()
	timeout := time.After(10 * time.Second)
	for !ok(tree) {
		select {
		case f := <-ss.frames:
			switch f := f.(type) {
			case ui.PatchFrame:
				for _, p := range f.Patches {
					tree = ui.ApplyPatch(tree, p)
				}
			case ui.TreeFrame:
				tree = f.Tree
			case ui.ErrorFrame:
				ss.t.Logf("error frame: %+v", f)
			}
		case <-timeout:
			ss.t.Fatalf("%s: never; the list is %s", what, summary(tree))
		}
	}
	return tree
}

func summary(n *ui.Node) string {
	if n == nil {
		return "nil"
	}
	return fmt.Sprintf("%s count %v offset %v rows %d", n.Type, n.Props["count"], n.Props["offset"], len(n.Children))
}

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return -1
}

func TestAVirtualListHoldsOnlyItsWindow(t *testing.T) {
	st := newRender(t)
	ids := st.items(300)
	st.do("prefab.create", "itemList", map[string]any{"name": "Item list"})
	st.insert("itemList", "", map[string]any{"type": "mui.virtualList", "key": "list", "props": map[string]any{"entity": "item", "pageSize": 20, "as": "item", "selection": "multiple"}})
	st.insert("itemList", "list", map[string]any{"type": "mui.text", "key": "title", "bind": map[string]any{"text": "item.title"}})
	st.publish("itemList")
	p := ui.PrefabPage("itemList", "", nil)
	p.ID, p.App, p.Title = "test/items", "test", "Items"
	ss := st.open(p)
	tree := ss.tree()
	if tree.Type != ui.VirtualListType {
		t.Fatalf("the page is %s", text(tree))
	}
	// The first window: a viewport of 20 and one below, two pages
	tree = ss.follow(tree, "the first window", func(n *ui.Node) bool { return num(n.Props["count"]) == 300 && len(n.Children) == 40 })
	if num(tree.Props["offset"]) != 0 || tree.Children[0].Key != tree.Key+"_"+ids[0] || tree.Children[0].Props["text"] != "Item 000" || tree.Children[39].Props["text"] != "Item 039" {
		t.Fatalf("the first window: %s; first %s", summary(tree), text(tree.Children[0]))
	}
	handler := tree.On["window"].Handler
	if !strings.HasPrefix(handler, "window:instances.") {
		t.Fatalf("the window handler is %q", handler)
	}
	held := func(prefix string) []string {
		var out []string
		for _, q := range ss.s.Queries() {
			if strings.Contains(q, prefix) {
				out = append(out, q)
			}
		}
		return out
	}
	if pages := held("#page:"); len(pages) != 2 {
		t.Errorf("the first window holds pages %v", pages)
	}

	// Far below: rows 200-219 shown, so rows 180-239, pages 9 to 11,
	// reached by skipping
	ss.s.Event(user(), handler, []string{}, map[string]any{"first": 200.0, "last": 219.0})
	tree = ss.follow(tree, "the far window", func(n *ui.Node) bool {
		return num(n.Props["offset"]) == 180 && len(n.Children) == 60 && n.Children[0].Props["text"] == "Item 180"
	})
	if last := tree.Children[59]; last.Props["text"] != "Item 239" {
		t.Errorf("the far window ends with %s", text(last))
	}
	pages := held("#page:")
	slices.Sort(pages)
	if len(pages) != 3 || !strings.HasSuffix(pages[0], "#page:10") && !strings.HasSuffix(pages[0], "#page:9") {
		t.Errorf("the far window holds pages %v, not 9 to 11", pages)
	}

	// Another session changes a row in the window: it is patched in place
	st.do("record.update", ids[205], map[string]any{"entity": "item", "set": map[string]any{"title": "Changed"}})
	tree = ss.follow(tree, "the changed row", func(n *ui.Node) bool {
		c := n.Child(n.Key + "_" + ids[205])
		return c != nil && c.Props["text"] == "Changed"
	})
	if len(tree.Children) != 60 || num(tree.Props["offset"]) != 180 {
		t.Errorf("after the change: %s", summary(tree))
	}

	// Back to the top: the pages held earlier come back by their cursors
	ss.s.Event(user(), handler, []string{}, map[string]any{"first": 0.0, "last": 19.0})
	tree = ss.follow(tree, "the top again", func(n *ui.Node) bool { return num(n.Props["offset"]) == 0 && len(n.Children) == 40 })
	if pages := held("#page:"); len(pages) != 2 {
		t.Errorf("back at the top, pages held: %v", pages)
	}
	raw, _ := json.Marshal(tree.Props["ids"])
	if !strings.Contains(string(raw), ids[0]) {
		t.Errorf("ids: %s", raw)
	}
}

func TestAVirtualGridSendsRecordsAsRows(t *testing.T) {
	st := newRender(t)
	st.items(45)
	st.do("prefab.create", "itemGrid", map[string]any{"name": "Item grid"})
	st.insert("itemGrid", "", map[string]any{"type": "mui.virtualGrid", "key": "grid", "props": map[string]any{"entity": "item", "pageSize": 10,
		"columns": []any{map[string]any{"field": "title", "title": "Title", "sortable": true}}, "sort": []any{map[string]any{"field": "title", "desc": true}}}})
	st.publish("itemGrid")
	p := ui.PrefabPage("itemGrid", "", nil)
	p.ID, p.App, p.Title = "test/grid", "test", "Grid"
	ss := st.open(p)
	tree := ss.follow(ss.tree(), "the grid's rows", func(n *ui.Node) bool {
		rows, _ := n.Props["rows"].([]any)
		return num(n.Props["count"]) == 45 && len(rows) == 20
	})
	rows := tree.Props["rows"].([]any)
	first, _ := rows[0].(map[string]any)
	if first["title"] != "Item 044" || len(tree.Children) != 0 {
		t.Errorf("the grid sorts by title, descending, with no children: first %v, %d children", first, len(tree.Children))
	}
	if _, ok := tree.Props["sort"]; !ok {
		t.Errorf("the grid keeps its sort for its headers")
	}
	if _, ok := tree.Props["entity"]; !ok {
		t.Errorf("entity stays a prop")
	}
}
