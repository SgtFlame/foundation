package hello_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// event sends a ui.event to session on the node at path.
func (c *client) event(session, handler string, path []string, payload any) {
	p, _ := json.Marshal(path)
	raw, _ := json.Marshal(payload)
	c.send(fmt.Sprintf(`{"type":"ui.event","session":%q,"handler":%q,"path":%s,"payload":%s}`, session, handler, p, raw))
}

// rows waits for the session's table rows to be as want, by title: its
// rows are patched as a bound prop.
func (c *client) rows(session string, want ...string) []map[string]any {
	c.t.Helper()
	timeout := time.After(5 * time.Second)
	var last string
	for {
		select {
		case m := <-c.s.out:
			var f ui.PatchFrame
			if json.Unmarshal(m, &f) != nil || f.Type != "ui.patch" || f.Session != session {
				continue
			}
			for _, p := range f.Patches {
				rows, ok := p.Props["rows"].([]any)
				if !ok {
					continue
				}
				var titles []string
				var out []map[string]any
				for _, r := range rows {
					rm, _ := r.(map[string]any)
					out = append(out, rm)
					titles = append(titles, fmt.Sprint(rm["title"]))
				}
				last = strings.Join(titles, ",")
				if last == strings.Join(want, ",") {
					return out
				}
			}
		case <-timeout:
			c.t.Fatalf("%s's rows never became %v; last %q", session, want, last)
			return nil
		}
	}
}

func (st *stack) task(title string) data.Record {
	st.t.Helper()
	res, err := data.Find(userArgs(), data.Deps{Projections: st.p}, data.Query{Entity: "task"})
	if err != nil {
		st.t.Fatal(err)
	}
	for _, r := range res.Records {
		if string(r["title"]) == fmt.Sprintf("%q", title) {
			return r
		}
	}
	st.t.Fatalf("no task %q", title)
	return nil
}

// The task list demo of forge 03i, as two users see it.
func TestTheTaskListWithTwoUsers(t *testing.T) {
	st := newStack(t)
	ada, bob := st.connect(1), st.connect(2)

	// Opening the list seeds and publishes Task
	ada.send(`{"type":"ui.open","session":"sa","page":"hello/tasks"}`)
	tree := ada.await(`"type":"ui.tree"`, `"session":"sa"`)
	if strings.Contains(tree, "ui.error") || !strings.Contains(tree, `"title":"Due"`) || !strings.Contains(tree, `"editable":true`) {
		t.Fatalf("the page is not the task list: %s", tree)
	}
	bob.send(`{"type":"ui.open","session":"sb","page":"hello/tasks"}`)
	bob.await(`"type":"ui.tree"`, `"session":"sb"`, `"title":"Assignee"`)

	// Ada adds a task assigned to herself; both see it, and the count
	add := []string{"add", "addTask"}
	ada.event("sa", "hello-onAddTask", add, map[string]any{"title": "Write the plan", "due": "2026-11-15", "mine": true})
	rows := bob.rows("sb", "Write the plan")
	assignee, _ := json.Marshal(rows[0]["_expanded"])
	if !strings.Contains(string(assignee), `"displayName":"Ada"`) || rows[0]["due"] != "2026-11-15" || rows[0]["done"] != false {
		t.Errorf("the new row: %v", rows[0])
	}
	ada.rows("sa", "Write the plan")
	bob.await(`"type":"ui.patch"`, `"session":"sb"`, `"label":"1 open"`)
	ada.event("sa", "hello-onAddTask", add, map[string]any{"title": "Ship it", "due": "2026-11-10"})
	bob.rows("sb", "Ship it", "Write the plan") // sorted by due
	ada.event("sa", "hello-onAddTask", add, map[string]any{"title": "  "})
	ada.await(`"type":"ui.patch"`, `Give the task a title`)

	// Bob shows open tasks only; Ada ticks one done and his list loses it
	bob.event("sb", "hello-onFilterTasks", []string{"tools", "show"}, map[string]any{"value": "open"})
	plan := st.task("Write the plan")
	var id string
	_ = json.Unmarshal(plan[data.MetaID], &id)
	ada.event("sa", "hello-onEditTask", []string{"list"}, map[string]any{"key": id, "field": "done", "value": true})
	bob.rows("sb", "Ship it")
	ada.rows("sa", "Ship it", "Write the plan")
	bob.await(`"type":"ui.patch"`, `"session":"sb"`, `"label":"1 open"`)

	// Sorting by title, descending, is Bob's own view
	bob.event("sb", "hello-onFilterTasks", []string{"tools", "show"}, map[string]any{"value": "all"})
	bob.rows("sb", "Ship it", "Write the plan")
	bob.event("sb", "hello-onSortTasks", []string{"list"}, map[string]any{"field": "title", "direction": "desc"})
	bob.rows("sb", "Write the plan", "Ship it")

	// Both toggle Ship it at the same moment: both toggles are decided in
	// turn, so it ends where it started, and both screens agree
	ship := st.task("Ship it")
	_ = json.Unmarshal(ship[data.MetaID], &id)
	var wg sync.WaitGroup
	for _, x := range []struct {
		c       *client
		session string
	}{{ada, "sa"}, {bob, "sb"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x.c.event(x.session, "hello-onEditTask", []string{"list"}, map[string]any{"key": id, "field": "done", "value": true})
		}()
	}
	wg.Wait()
	// Two toggles: it was open, then done, then open again
	var ship2 data.Record
	for deadline := time.Now().Add(5 * time.Second); ; {
		if err := st.p.Settle(context.Background(), features.LocalTenantID, data.Projector); err != nil {
			t.Fatal(err)
		}
		ship2 = st.task("Ship it")
		if string(ship2["done"]) == "false" && ship2.Seq() > ship.Seq()+1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("two toggles should leave it open, two events on: %s, seq %d → %d", ship2["done"], ship.Seq(), ship2.Seq())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Each screen's last rows show it open again
	for _, x := range []struct {
		c       *client
		session string
	}{{ada, "sa"}, {bob, "sb"}} {
		deadline := time.After(5 * time.Second)
		for done := false; !done; {
			select {
			case m := <-x.c.s.out:
				var f ui.PatchFrame
				if json.Unmarshal(m, &f) != nil || f.Session != x.session {
					continue
				}
				for _, p := range f.Patches {
					if rows, ok := p.Props["rows"].([]any); ok {
						for _, r := range rows {
							rm := r.(map[string]any)
							if rm["title"] == "Ship it" && rm["done"] == false && fmt.Sprint(rm["_seq"]) == fmt.Sprint(ship2.Seq()) {
								done = true
							}
						}
					}
				}
			case <-deadline:
				t.Fatalf("%s never showed Ship it open again at %d", x.session, ship2.Seq())
			}
		}
	}

	// Clear done deletes the done tasks in one command
	ada.event("sa", "hello-onClearDone", []string{"tools", "clearDone"}, nil)
	bob.rows("sb", "Ship it")

	// A field added to Task and published becomes a column, as the chat's
	// tools would add it: entity.addField, then entity.publish
	st.do("entity.addField", "task", map[string]any{"name": "priority", "type": "enum", "default": "medium", "options": []any{
		map[string]any{"id": "low", "name": "Low"}, map[string]any{"id": "medium", "name": "Medium"}, map[string]any{"id": "high", "name": "High"}}})
	v, err := st.p.WaitFor(context.Background(), 1, "records.entity", "entity.task", st.seq["task"], 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var d struct{ Rev int }
	_ = json.Unmarshal(v.Value, &d)
	st.do("entity.publish", "task", map[string]int{"rev": d.Rev})
	bob.await(`"type":"ui.patch"`, `"session":"sb"`, `"title":"priority"`, `"label":"Medium"`)
	ada.event("sa", "hello-onAddTask", add, map[string]any{"title": "Prioritised"})
	rows = bob.rows("sb", "Ship it", "Prioritised")
	if rows[1]["priority"] != "medium" {
		t.Errorf("a new task should get the default priority: %v", rows[1])
	}
}
