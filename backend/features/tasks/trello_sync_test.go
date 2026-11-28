package tasks_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/tasks"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trello"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trello/trellosim"
)

// Forge 30i: the two-way sync of 30h with a Trello board, the tasks.trello
// type over the Trello simulator. Connecting imports the board's 20 cards;
// a card moved and renamed in Trello moves and renames its task; a task
// moved and renamed here moves and renames its card.
func TestTrelloSync(t *testing.T) {
	f := setup(t)
	sim := trellosim.New()
	board, lists := sim.Seed(trellosim.Board{Name: "Launch", Lists: []string{"To do", "Doing", "Done"}, Cards: 20})
	srv := httptest.NewServer(sim)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := connections.Serve(ctx, f.srv.Conn(), f.connDeps); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = adapter.Serve(ctx, f.srv.Conn(), adapter.Options{Name: "trello", Types: []connectors.Type{trello.Type()}, Contracts: []connectors.Contract{tracker.Contract()},
			Impls: []adapter.Impl{trello.Impl(trello.Options{Base: srv.URL, Poll: 100 * time.Millisecond})}})
	}()
	go connections.Start(ctx, f.connDeps, func() ([]uint64, error) { return []uint64{features.LocalTenantID}, nil })
	res := f.doAs(user(owner), "connection.create", "", map[string]any{"type": trello.TypeID, "name": "Trello", "owner": "tenant", "settings": map[string]any{}, "secrets": map[string]any{"key": "k", "token": "t"}})
	if res.Common.Code >= 300 {
		t.Fatalf("connection.create: %s %+v", res.Common.Message, res.Problems)
	}
	conn := res.Target
	eventually(t, "Trello reached", 10*time.Second, func() bool {
		_, e := f.tracker(conn, tracker.Ref, "check", map[string]any{})
		return e == nil
	})

	web := f.newProject(owner, "Launch", nil, nil)
	f.okOn(owner, "record.update", tasks.EntityProject, web, map[string]any{"entity": tasks.EntityProject, "set": map[string]any{"connection": conn, "board": board}})
	eventually(t, "the import", 30*time.Second, func() bool {
		s, _ := f.get(tasks.EntityProject, web)["sync"].(map[string]any)
		return s != nil && s["state"] == "ok"
	})
	all := f.find(user(owner), tasks.EntityTask, map[string]any{"project": web})
	if len(all) != 20 {
		t.Fatalf("%d tasks for 20 cards", len(all))
	}
	status := map[string]string{} // list id → status id
	for _, s := range f.find(user(owner), tasks.EntityStatus, map[string]any{"project": web}) {
		if l := str(s["outsideList"]); l != "" {
			status[l] = fmt.Sprint(s["_id"])
		}
	}
	if len(status) != 3 {
		t.Fatalf("mapped lists %v", status)
	}

	// Moved and renamed in Trello
	card := sim.Cards(board)[0]
	tk := ""
	for _, x := range all {
		if x["outsideId"] == card.ID {
			tk = fmt.Sprint(x["_id"])
		}
	}
	sim.Change(card.ID, map[string]any{"idList": lists[2], "name": "Renamed in Trello"})
	eventually(t, "Trello's change on the task", 20*time.Second, func() bool {
		x := f.get(tasks.EntityTask, tk)
		return x["title"] == "Renamed in Trello" && x["status"] == status[lists[2]]
	})

	// Moved and renamed here
	f.okOn(owner, "record.update", tasks.EntityTask, tk, map[string]any{"entity": tasks.EntityTask, "set": map[string]any{"title": "Renamed here"}})
	f.okOn(owner, "tasks.task.move", tasks.EntityTask, tk, map[string]any{"status": status[lists[1]]})
	eventually(t, "our change on the card", 20*time.Second, func() bool {
		for _, c := range sim.Cards(board) {
			if c.ID == card.ID {
				return c.Name == "Renamed here" && c.IDList == lists[1]
			}
		}
		return false
	})
	time.Sleep(time.Second)
	if x := f.get(tasks.EntityTask, tk); x["title"] != "Renamed here" || x["status"] != status[lists[1]] {
		t.Errorf("the task after the echo: %v in %v", x["title"], x["status"])
	}
}
