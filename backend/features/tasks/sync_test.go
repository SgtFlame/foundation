package tasks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/tasks"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trackerfake"
)

func init() {
	connectors.LeaseTTL = 1500 * time.Millisecond
}

// memSecrets is a secret store for connections with no secrets.
type memSecrets struct{}

func (memSecrets) Put(_ context.Context, ref config.SecretRef, _ string) (map[string]any, error) {
	return map[string]any{"$mem": ref.Name()}, nil
}
func (memSecrets) Get(context.Context, config.SecretRef, map[string]any) (string, error) {
	return "", fmt.Errorf("no secrets here")
}
func (memSecrets) Delete(context.Context, config.SecretRef) error { return nil }
func (memSecrets) Hints() bool                                     { return false }

// serveTracker serves the Tasks test server's type in this process, with
// Foundation's answers to adapters and the event sources, and makes a
// tenant connection to it.
func (f *fixture) serveTracker() string {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	if err := connections.Serve(ctx, f.srv.Conn(), f.connDeps); err != nil {
		f.t.Fatal(err)
	}
	go func() { _ = adapter.Serve(ctx, f.srv.Conn(), trackerfake.New().Options("tasks-fake")) }()
	go connections.Start(ctx, f.connDeps, func() ([]uint64, error) { return []uint64{features.LocalTenantID}, nil })
	res := f.doAs(user(owner), "connection.create", "", map[string]any{"type": trackerfake.TypeID, "name": "Tracker", "owner": "tenant", "settings": map[string]any{}})
	if res.Common.Code >= 300 {
		f.t.Fatalf("connection.create: %s %+v", res.Common.Message, res.Problems)
	}
	eventually(f.t, "the test server online", 10*time.Second, func() bool {
		_, e := f.tracker(res.Target, trackerfake.ControlRef, "state", map[string]any{})
		return e == nil
	})
	return res.Target
}

// tracker calls the test server as the tenant's owner.
func (f *fixture) tracker(conn, contract, op string, args any) (map[string]any, *connectors.Error) {
	raw, _ := json.Marshal(args)
	out, e := f.dispatcher.Call(user(owner), connectors.Call{Contract: contract, Operation: op, Connection: conn, Args: raw})
	if e != nil {
		return nil, e
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m, nil
}

func (f *fixture) mustTracker(conn, contract, op string, args any) map[string]any {
	f.t.Helper()
	m, e := f.tracker(conn, contract, op, args)
	if e != nil {
		f.t.Fatalf("%s %s: %s %s", contract, op, e.Code, e.Message)
	}
	return m
}

// Forge 30h: two-way sync with a tracker board, on the Tasks test server.
// Connecting a project imports the board's 500 cards once, mapping its
// lists to statuses (a list with no status gets one). A card changed in
// the tracker reaches its task once and is not pushed back; a task
// changed here reaches its card once and the tracker's echo is dropped.
// When both sides change one task, the later change is kept and the
// other logged: once ours, which goes back to the card, once theirs.
func TestTwoWaySync(t *testing.T) {
	f := setup(t)
	conn := f.serveTracker()
	f.mustTracker(conn, trackerfake.ControlRef, "seed", map[string]any{
		"board": map[string]any{"id": "b1", "name": "Website relaunch", "lists": []string{"To do", "Doing", "Blocked", "Done"}}, "count": 500})

	web := f.newProject(owner, "Website relaunch", nil, nil)
	f.okOn(owner, "record.update", tasks.EntityProject, web, map[string]any{"entity": tasks.EntityProject, "set": map[string]any{"connection": conn, "board": "b1"}})

	// The import: every card once, the lists mapped, Blocked made
	eventually(t, "the import", 60*time.Second, func() bool {
		p := f.get(tasks.EntityProject, web)
		s, _ := p["sync"].(map[string]any)
		return s != nil && s["state"] == "ok"
	})
	all := f.find(user(owner), tasks.EntityTask, map[string]any{"project": web})
	cards := map[string]map[string]any{}
	for _, tk := range all {
		cards[fmt.Sprint(tk["outsideId"])] = tk
	}
	if len(all) != 500 || len(cards) != 500 {
		t.Fatalf("%d tasks for %d cards", len(all), len(cards))
	}
	sts := f.find(user(owner), tasks.EntityStatus, map[string]any{"project": web})
	lists := map[string]string{}
	for _, s := range sts {
		if l := str(s["outsideList"]); l != "" {
			lists[fmt.Sprint(s["name"])] = l
		}
	}
	if len(sts) != 5 || len(lists) != 4 || lists["Blocked"] == "" {
		t.Errorf("statuses %v, mapped %v", len(sts), lists)
	}
	if p := f.get(tasks.EntityProject, web); fmt.Sprint(p["sync"].(map[string]any)["imported"]) != "500" {
		t.Errorf("the project's sync: %v", p["sync"])
	}

	// An outside change reaches its task once, and is not pushed back
	var one map[string]any
	var cardID string
	for id, tk := range cards {
		cardID, one = id, tk
		break
	}
	v := f.mustTracker(conn, trackerfake.ControlRef, "change", map[string]any{"card": cardID, "set": map[string]any{"title": "Changed outside"}})["version"]
	eventually(t, "the outside change", 20*time.Second, func() bool {
		return f.get(tasks.EntityTask, fmt.Sprint(one["_id"]))["title"] == "Changed outside"
	})
	tid := fmt.Sprint(one["_id"])
	if got := f.get(tasks.EntityTask, tid); got["outside"].(map[string]any)["version"] != v {
		t.Errorf("the task holds %v, the card is at %v", got["outside"], v)
	}
	time.Sleep(time.Second)
	if c := f.mustTracker(conn, tracker.Ref, "card.get", map[string]any{"id": cardID}); c["version"] != v {
		t.Errorf("the pulled change was pushed back: the card is at %v, was %v", c["version"], v)
	}

	// Our change reaches the card once; its echo changes nothing here
	f.okOn(owner, "record.update", tasks.EntityTask, tid, map[string]any{"entity": tasks.EntityTask, "set": map[string]any{"title": "Changed here"}})
	var pushed string
	eventually(t, "our change on the card", 20*time.Second, func() bool {
		c := f.mustTracker(conn, tracker.Ref, "card.get", map[string]any{"id": cardID})
		pushed = fmt.Sprint(c["version"])
		return c["title"] == "Changed here"
	})
	eventually(t, "the task holding the card's new version", 10*time.Second, func() bool {
		return f.get(tasks.EntityTask, tid)["outside"].(map[string]any)["version"] == pushed
	})
	time.Sleep(time.Second)
	if c := f.mustTracker(conn, tracker.Ref, "card.get", map[string]any{"id": cardID}); fmt.Sprint(c["version"]) != pushed {
		t.Errorf("the card changed again after our push: %v, pushed %v", c["version"], pushed)
	}

	// Both sides, ours later: the card changes while the pull cannot read
	// it, then we change the task; ours is kept, logged, and pushed back
	f.mustTracker(conn, trackerfake.ControlRef, "fail", map[string]any{"operation": "card.get", "kind": "unavailable", "times": 2})
	f.mustTracker(conn, trackerfake.ControlRef, "change", map[string]any{"card": cardID, "set": map[string]any{"title": "Theirs, earlier"}})
	time.Sleep(50 * time.Millisecond)
	f.okOn(owner, "record.update", tasks.EntityTask, tid, map[string]any{"entity": tasks.EntityTask, "set": map[string]any{"title": "Ours, later"}})
	eventually(t, "ours on the card", 30*time.Second, func() bool {
		c, e := f.tracker(conn, tracker.Ref, "card.get", map[string]any{"id": cardID}) // may take one of the failures meant for the pull
		return e == nil && c["title"] == "Ours, later"
	})
	eventually(t, "the conflict logged", 10*time.Second, func() bool {
		for _, l := range f.find(user(owner), "taskSyncLog", map[string]any{"project": web}) {
			if l["kind"] == "conflict" && l["kept"] == "ours" && strings.Contains(fmt.Sprint(l["lost"]), "Theirs, earlier") {
				return true
			}
		}
		return false
	})
	if got := f.get(tasks.EntityTask, tid)["title"]; got != "Ours, later" {
		t.Errorf("the task's title %v", got)
	}

	// Both sides, theirs later: our push cannot reach the card, then the
	// card changes; theirs is kept and ours logged
	f.mustTracker(conn, trackerfake.ControlRef, "fail", map[string]any{"operation": "card.update", "kind": "unavailable", "times": 3})
	f.okOn(owner, "record.update", tasks.EntityTask, tid, map[string]any{"entity": tasks.EntityTask, "set": map[string]any{"title": "Ours, earlier"}})
	time.Sleep(50 * time.Millisecond)
	f.mustTracker(conn, trackerfake.ControlRef, "change", map[string]any{"card": cardID, "set": map[string]any{"title": "Theirs, later"}})
	eventually(t, "theirs on the task", 30*time.Second, func() bool {
		return f.get(tasks.EntityTask, tid)["title"] == "Theirs, later"
	})
	eventually(t, "that conflict logged", 10*time.Second, func() bool {
		for _, l := range f.find(user(owner), "taskSyncLog", map[string]any{"project": web}) {
			if l["kind"] == "conflict" && l["kept"] == "theirs" && strings.Contains(fmt.Sprint(l["lost"]), "Ours, earlier") {
				return true
			}
		}
		return false
	})
	time.Sleep(3 * time.Second) // the failed push retries, and finds nothing of ours to send
	if c := f.mustTracker(conn, tracker.Ref, "card.get", map[string]any{"id": cardID}); c["title"] != "Theirs, later" {
		t.Errorf("the card's title %v", c["title"])
	}

	// A task made here gets a card in its status's list
	made := fmt.Sprint(time.Now().UnixNano())
	f.okOn(owner, "tasks.task.create", tasks.EntityTask, made, map[string]any{"project": web, "title": "Made here"})
	eventually(t, "the new task's card", 20*time.Second, func() bool {
		return str(f.get(tasks.EntityTask, made)["outsideId"]) != ""
	})
	c := f.mustTracker(conn, tracker.Ref, "card.get", map[string]any{"id": f.get(tasks.EntityTask, made)["outsideId"]})
	if c["title"] != "Made here" || c["list"] != lists["To do"] || c["key"] != made {
		t.Errorf("the new card: %v", c)
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
