package hello_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// event sends a ui.event to session on the node at path.
func (c *client) event(session, handler string, path []string, payload any) {
	p, _ := json.Marshal(path)
	raw, _ := json.Marshal(payload)
	c.send(fmt.Sprintf(`{"type":"ui.event","session":%q,"handler":%q,"path":%s,"payload":%s}`, session, handler, p, raw))
}

// view is a session's tree as the browser holds it: the first tree, with
// every patch applied as UiSession applies them.
type view struct {
	c       *client
	session string
	tree    *ui.Node
	toasts  []string
}

func (c *client) open(session, page string) *view {
	c.t.Helper()
	// As the browser opens a tab: its params hold the tab's id
	c.send(fmt.Sprintf(`{"type":"ui.open","session":%q,"page":%q,"params":{"id":"main"}}`, session, page))
	v := &view{c: c, session: session}
	v.until("the first tree", func(*ui.Node) bool { return true })
	return v
}

// until applies frames until ok holds for the tree.
func (v *view) until(what string, ok func(*ui.Node) bool) *ui.Node {
	v.c.t.Helper()
	if v.tree != nil && ok(v.tree) {
		return v.tree
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-v.c.s.out:
			var f struct {
				Type, Session, Message string
				Tree                   *ui.Node
				Patches                []ui.Patch
				Actions                []ui.Action
			}
			if json.Unmarshal(m, &f) != nil || f.Session != v.session {
				continue
			}
			switch f.Type {
			case "ui.tree":
				v.tree = f.Tree
			case "ui.patch":
				for _, p := range f.Patches {
					v.apply(p)
				}
				for _, a := range f.Actions {
					if a.Type == "toast" {
						v.toasts = append(v.toasts, a.Message)
					}
				}
			case "ui.error":
				v.c.t.Logf("%s: ui.error %s", v.session, f.Message)
			}
			if v.tree != nil && ok(v.tree) {
				return v.tree
			}
		case <-timeout:
			raw, _ := json.Marshal(v.tree)
			v.c.t.Fatalf("%s never showed %s; its tree: %s", v.session, what, raw)
			return nil
		}
	}
}

func (v *view) apply(p ui.Patch) {
	parentOf := func(path []string) (*ui.Node, string) {
		return v.tree.At(path[:len(path)-1]), path[len(path)-1]
	}
	switch p.Op {
	case "replace":
		if len(p.Path) == 0 {
			v.tree = p.Node
			return
		}
		parent, key := parentOf(p.Path)
		if i := slices.IndexFunc(parent.Children, func(c *ui.Node) bool { return c.Key == key }); i >= 0 {
			parent.Children[i] = p.Node
		}
	case "insert":
		parent := v.tree.At(p.Path)
		parent.Children = slices.Insert(parent.Children, min(p.Index, len(parent.Children)), p.Node)
	case "remove":
		parent, key := parentOf(p.Path)
		parent.Children = slices.DeleteFunc(parent.Children, func(c *ui.Node) bool { return c.Key == key })
	case "move":
		parent := v.tree.At(p.Path)
		i := slices.IndexFunc(parent.Children, func(c *ui.Node) bool { return c.Key == p.Key })
		n := parent.Children[i]
		parent.Children = slices.Insert(slices.Delete(parent.Children, i, i+1), p.Index, n)
	case "props":
		n := v.tree.At(p.Path)
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		for k, x := range p.Props {
			if x == nil {
				delete(n.Props, k)
			} else {
				n.Props[k] = x
			}
		}
	}
}

// rows are the list's Task rows, in order.
func rows(tree *ui.Node) []*ui.Node {
	var out []*ui.Node
	if list := tree.At([]string{"list"}); list != nil {
		for _, c := range list.Children {
			if strings.HasPrefix(c.Key, "rows_") {
				out = append(out, c)
			}
		}
	}
	return out
}

func titleOf(row *ui.Node) string {
	if t := row.Child("title"); t != nil {
		s, _ := t.Props["value"].(string)
		return s
	}
	return ""
}

// showing waits until the list's rows are want, by title.
func (v *view) showing(want ...string) []*ui.Node {
	v.c.t.Helper()
	tree := v.until(fmt.Sprintf("the rows %v", want), func(tree *ui.Node) bool {
		var titles []string
		for _, r := range rows(tree) {
			titles = append(titles, titleOf(r))
		}
		return slices.Equal(titles, want)
	})
	return rows(tree)
}

// row is the row titled title, which the view shows.
func (v *view) row(title string) *ui.Node {
	v.c.t.Helper()
	for _, r := range rows(v.tree) {
		if titleOf(r) == title {
			return r
		}
	}
	v.c.t.Fatalf("%s shows no row %q", v.session, title)
	return nil
}

// send sends the event a node of the tree maps, from the node at path,
// with the payload the browser would fill in: the mapping's literal values
// as the server rendered them, and event.* from event.
func (v *view) send(path []string, name string, event map[string]any) {
	v.c.t.Helper()
	n := v.tree.At(path)
	if n == nil || n.On[name].Handler == "" {
		v.c.t.Fatalf("%s has no %s event at %v", v.session, name, path)
	}
	on := n.On[name]
	payload := map[string]any{}
	for k, x := range on.Payload {
		if s, ok := x.(string); ok && strings.HasPrefix(s, "event.") {
			payload[k] = event[s[6:]]
		} else if !ok || !strings.HasPrefix(s, "local.") {
			payload[k] = x
		}
	}
	if len(on.Payload) == 0 {
		payload = event
	}
	v.c.event(v.session, on.Handler, path, payload)
}

// add sends the add form, as its button does with the local values typed.
func (v *view) add(title, due string, mine bool) {
	v.c.t.Helper()
	n := v.tree.At([]string{"add", "addTask"})
	v.c.event(v.session, n.On["click"].Handler, []string{"add", "addTask"}, map[string]any{"title": title, "due": due, "mine": mine})
}

func (st *stack) task(title string) data.Record {
	st.t.Helper()
	r, titles := st.find(title)
	if r == nil {
		st.t.Fatalf("no task %q in %v", title, titles)
	}
	return r
}

// find looks for a task by title, giving the titles there are when there
// is none.
func (st *stack) find(title string) (data.Record, []string) {
	st.t.Helper()
	res, err := data.Find(userArgs(), data.Deps{Projections: st.p}, data.Query{Entity: "task"})
	if err != nil {
		st.t.Fatal(err)
	}
	var titles []string
	for _, r := range res.Records {
		if string(r["title"]) == fmt.Sprintf("%q", title) {
			return r, nil
		}
		titles = append(titles, string(r["title"]))
	}
	return nil, titles
}

// publishPrefab publishes a prefab's draft at the rev it is at.
func (st *stack) publishPrefab(id string) {
	st.t.Helper()
	v, err := st.p.WaitFor(context.Background(), 1, prefabs.DraftProjector, "prefab."+id, st.seq[id], 2*time.Second)
	if err != nil {
		st.t.Fatal(err)
	}
	var d struct{ Rev int }
	_ = json.Unmarshal(v.Value, &d)
	st.do("prefab.publish", id, map[string]int{"rev": d.Rev})
}

// The demo of forge 04d with two users: Hello's task page is the Task list
// prefab, which Hello made with prefab.* commands; new, toggled, renamed
// and cleared tasks reach both, the chip colour changed and republished
// reaches every open list, and a new field becomes a chip by prefab ops.
// (Hello seeds once per process, so the whole demo is one test.)
func TestTheTaskListPrefabsWithTwoUsers(t *testing.T) {
	st := newStack(t)
	ada, bob := st.connect(1), st.connect(2)

	// Opening the list seeds Task and both prefabs, and publishes them
	a := ada.open("sa", "hello/tasks")
	raw, _ := json.Marshal(a.tree)
	if strings.Contains(string(raw), "ui.error") || !strings.Contains(string(raw), `"text":"Tasks"`) || !strings.Contains(string(raw), "prefab:root#onAdd") {
		t.Fatalf("the page is not Task list: %s", raw)
	}
	if strings.Contains(string(raw), "mui.table") {
		t.Fatal("the page still has the Go page's table")
	}
	if a.tree.At([]string{"list", "empty"}) == nil {
		t.Errorf("an empty list shows no empty state: %s", raw)
	}
	for _, id := range []string{"taskRow", "taskList"} {
		data, _, fail := st.subs.Once(userArgs(), "prefab.describe", json.RawMessage(fmt.Sprintf(`{"id":%q,"published":true}`, id)))
		if fail != nil {
			t.Fatalf("%s is not published: %s", id, fail.Message)
		}
		raw, _ := json.Marshal(data)
		if !strings.Contains(string(raw), `"owner":"app:hello"`) {
			t.Errorf("%s is not Hello's: %s", id, raw)
		}
	}
	b := bob.open("sb", "hello/tasks")

	// Ada adds a task assigned to herself; both see it, and the count
	a.add("Write the plan", "2026-11-15", true)
	row := b.showing("Write the plan")[0]
	if got := row.At([]string{"assignee"}); got == nil || got.Props["text"] != "Ada" {
		t.Errorf("the row's assignee: %+v", got)
	}
	if due := row.At([]string{"due"}); due == nil || due.Props["label"] != "2026-11-15" || due.Props["color"] != "primary" {
		t.Errorf("the row's due chip: %+v", due)
	}
	if check := row.At([]string{"check"}); check == nil || check.Props["checked"] != false {
		t.Errorf("the row's checkbox: %+v", check)
	}
	a.showing("Write the plan")
	b.until("1 open", func(tree *ui.Node) bool { return tree.At([]string{"head", "open"}).Props["label"] == "1 open" })
	if b.tree.At([]string{"list", "empty"}) != nil {
		t.Error("the empty state stays with a task")
	}
	a.add("Ship it", "2026-11-10", false)
	b.showing("Ship it", "Write the plan") // sorted by due
	if b.row("Ship it").At([]string{"assignee"}) != nil {
		t.Error("a task with no assignee shows one")
	}
	a.add("  ", "", false)
	a.until("a toast", func(*ui.Node) bool { return slices.Contains(a.toasts, "Give the task a title.") })

	// Bob shows open tasks only; Ada ticks one done and his list loses it
	b.send([]string{"tools", "show"}, "change", map[string]any{"value": "open"})
	b.until("Show open", func(tree *ui.Node) bool { return tree.At([]string{"tools", "show"}).Props["value"] == "open" })
	plan := a.row("Write the plan")
	a.send([]string{"list", plan.Key, "check"}, "change", map[string]any{"value": true})
	b.showing("Ship it")
	a.until("Write the plan done", func(tree *ui.Node) bool {
		r := tree.At([]string{"list", plan.Key, "check"})
		return r != nil && r.Props["checked"] == true
	})
	a.showing("Ship it", "Write the plan")

	// Sorting is Bob's own: by title, then the direction flipped
	b.send([]string{"tools", "show"}, "change", map[string]any{"value": "all"})
	b.showing("Ship it", "Write the plan")
	b.send([]string{"tools", "sortBy"}, "change", map[string]any{"value": "title"})
	b.until("Sort by title", func(tree *ui.Node) bool { return tree.At([]string{"tools", "sortBy"}).Props["value"] == "title" })
	b.send([]string{"tools", "direction"}, "click", nil)
	b.showing("Write the plan", "Ship it")
	b.until("Descending", func(tree *ui.Node) bool {
		d := tree.At([]string{"tools", "direction"})
		return d.Props["label"] == "Descending" && d.Props["icon"] == "arrow_downward"
	})
	a.showing("Ship it", "Write the plan")

	// Ada renames a task in its row; Bob sees the new title in place
	ship := a.row("Ship it")
	a.send([]string{"list", ship.Key, "title"}, "blur", map[string]any{"value": "  Ship it today "})
	b.showing("Write the plan", "Ship it today")
	a.send([]string{"list", ship.Key, "title"}, "blur", map[string]any{"value": ""}) // left empty: nothing changes

	// Both toggle Ship it today at the same moment: both toggles are
	// decided in turn, so it ends where it started
	before := st.task("Ship it today")
	toggle := a.tree.At([]string{"list", ship.Key, "check"}).On["change"].Handler
	var wg sync.WaitGroup
	for _, v := range []*view{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.c.event(v.session, toggle, []string{"list", ship.Key, "check"}, map[string]any{"value": true})
		}()
	}
	wg.Wait()
	for deadline := time.Now().Add(5 * time.Second); ; {
		if err := st.p.Settle(context.Background(), features.LocalTenantID, data.Projector); err != nil {
			t.Fatal(err)
		}
		// A scan may miss a record rewritten while it lists the keys, as
		// the toggles may still be committing: look again
		after, _ := st.find("Ship it today")
		if after != nil && string(after["done"]) == "false" && after.Seq() > before.Seq()+1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("two toggles should leave it open, two events on: %v, seq %d", after, before.Seq())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Clear done deletes the done tasks in one command
	a.send([]string{"tools", "clearDone"}, "click", nil)
	b.showing("Ship it today")
	a.showing("Ship it today")

	// Task row's chip colour, changed and republished, reaches every open
	// list at once (demo step 4)
	st.do("prefab.updateNode", "taskRow", map[string]any{"path": "root/hasDue/due", "props": map[string]any{"color": "secondary"}})
	st.publishPrefab("taskRow")
	for _, v := range []*view{a, b} {
		v.until("the chip in secondary", func(tree *ui.Node) bool {
			r := rows(tree)
			return len(r) == 1 && r[0].At([]string{"due"}) != nil && r[0].At([]string{"due"}).Props["color"] == "secondary"
		})
	}

	// A field added to Task, as the chat's tools would add it, shows as a
	// column only once Task row shows it, by prefab ops
	st.do("entity.addField", "task", map[string]any{"name": "priority", "type": "enum", "default": "medium", "options": []any{
		map[string]any{"id": "low", "name": "Low"}, map[string]any{"id": "medium", "name": "Medium"}, map[string]any{"id": "high", "name": "High"}}})
	v, err := st.p.WaitFor(context.Background(), 1, "records.entity", "entity.task", st.seq["task"], 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var d struct{ Rev int }
	_ = json.Unmarshal(v.Value, &d)
	st.do("entity.publish", "task", map[string]int{"rev": d.Rev})
	st.do("prefab.insertNode", "taskRow", map[string]any{"parentPath": "root", "index": 4, "node": map[string]any{
		"type": "when", "key": "hasPriority", "props": map[string]any{"test": "props.task.priority"}, "slots": map[string]any{"then": []any{
			map[string]any{"type": "mui.chip", "key": "priority", "props": map[string]any{"label": "", "variant": "outlined"}, "bind": map[string]any{"label": "props.task.priority"}}}}}})
	st.publishPrefab("taskRow")
	a.add("Prioritised", "", false)
	b.showing("Ship it today", "Prioritised") // still Bob's descending title order
	b.until("the priority chip", func(*ui.Node) bool {
		p := b.row("Prioritised").At([]string{"priority"})
		return p != nil && p.Props["label"] == "medium"
	})
	if b.row("Ship it today").At([]string{"priority"}) != nil {
		t.Error("a task from before the field has no priority, so no chip")
	}
}
