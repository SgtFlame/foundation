package prefabs_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// A board's card template is evaluated per card under as, keyed by the
// card's id, and the cards go to the browser only as placed {id, column,
// version, key} (forge 30b).
func TestABoardEvaluatesItsCardTemplate(t *testing.T) {
	st := newRender(t)
	st.do("prefab.create", "board", map[string]any{"name": "Board"})
	st.do("prefab.setInterface", "board", map[string]any{"props": map[string]any{"items": map[string]any{"type": "json"}}})
	st.insert("board", "", map[string]any{"type": "mui.kanban", "key": "root",
		"props":    map[string]any{"columnField": "status", "columns": []any{map[string]any{"id": "todo", "title": "To do"}, map[string]any{"id": "done", "title": "Done"}}},
		"bind":     map[string]any{"cards": "=props.items"},
		"children": []any{map[string]any{"type": "mui.text", "key": "title", "bind": map[string]any{"text": "card.title"}}}})
	st.rev(prefabs.DraftProjector, "prefab.board", "board")
	items := []any{
		map[string]any{"_id": "t1", "_seq": 4, "title": "Write the plan", "status": "todo"},
		map[string]any{"_id": "t2", "_seq": 9, "title": "Ship it", "status": map[string]any{"_id": "done"}},
		map[string]any{"_id": "t1", "title": "A duplicate", "status": "done"},
	}
	tree, problems, fail := ui.Preview(user(), ui.Options{Catalog: ui.Catalog(st.reg), Subs: st.subs}, "board", "", map[string]any{"items": items}, true)
	if fail != nil || len(problems) > 0 {
		t.Fatalf("%v %+v", fail, problems)
	}
	if tree.Type != "mui.kanban" {
		t.Fatalf("root %s", text(tree))
	}
	if _, ok := tree.Props["cards"]; ok {
		t.Errorf("the cards were sent: %s", text(tree))
	}
	placed, _ := json.Marshal(tree.Props["placed"])
	if string(placed) != `[{"column":"todo","id":"t1","key":"root_t1","version":4},{"column":"done","id":"t2","key":"root_t2","version":9}]` {
		t.Errorf("placed %s", placed)
	}
	var keys []string
	for _, c := range tree.Children {
		keys = append(keys, c.Key+":"+c.Type+":"+text(c))
	}
	if len(keys) != 2 || !strings.HasPrefix(keys[0], "root_t1:mui.text") || !strings.Contains(keys[0], "Write the plan") || !strings.Contains(keys[1], "Ship it") {
		t.Errorf("cards %v", keys)
	}
}

// The check knows the card template reads its card under as, and that the
// board takes one child and emits move.
func TestTheCheckKnowsABoard(t *testing.T) {
	cat := ui.Catalog(newRender(t).reg)
	p := ui.Prefab{Name: "B", Template: &ui.Node{Type: "mui.kanban", Key: "root", Props: map[string]any{"as": "task"},
		On:       map[string]ui.On{"drop": {Handler: "onMove"}},
		Children: []*ui.Node{{Type: "mui.text", Key: "t", Bind: map[string]string{"text": "task.title"}}}}}
	var msgs []string
	for _, pr := range ui.CheckPrefab(p, ui.PrefabEnv{Catalog: cat, Prefab: func(string) (ui.Prefab, bool) { return ui.Prefab{}, false }, Uses: func(string) []string { return nil }, Entity: func(string) bool { return false }, Query: func(string) bool { return false }}) {
		msgs = append(msgs, pr.Path+": "+pr.Message)
	}
	all := strings.Join(msgs, "\n")
	if strings.Contains(all, "task.title") {
		t.Errorf("the card's row was not known:\n%s", all)
	}
	if !strings.Contains(all, `emits no event "drop"`) {
		t.Errorf("an unknown event was taken:\n%s", all)
	}
}
