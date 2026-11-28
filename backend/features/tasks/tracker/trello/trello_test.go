package trello_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/cassette"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trackertest"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trello"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trello/trellosim"
)

// base is the cassettes' API root.
const base = "https://api.trello.test"

// board is the cassettes' board.
const board = "656400000000000000000b01"

func seeded() *trellosim.Server {
	sim := trellosim.New()
	sim.Seed(trellosim.Board{ID: board, Name: "Website relaunch", Lists: []string{"To do", "Doing", "Done"}, Labels: []string{"Launch"}, Members: []string{"Grace Hopper"}, Cards: 5})
	return sim
}

// session replays file. TRELLO_CASSETTE=write writes it again from the
// simulator (trellosim, written from Trello's API reference);
// TRACKER_RECORD=1 records it from a real board (TRELLO_KEY, TRELLO_TOKEN
// and TRELLO_BOARD, deferred until Dec 15), rebased onto base.
func session(t *testing.T, file string) (*trello.Session, string) {
	t.Helper()
	switch {
	case os.Getenv("TRACKER_RECORD") == "1":
		key, token, b := os.Getenv("TRELLO_KEY"), os.Getenv("TRELLO_TOKEN"), os.Getenv("TRELLO_BOARD")
		if key == "" || token == "" || b == "" {
			t.Skip("recording needs TRELLO_KEY, TRELLO_TOKEN and TRELLO_BOARD")
		}
		r := cassette.Record(t, file, "trello", "recorded "+time.Now().UTC().Format("2006-01-02"), nil)
		r.From, r.To = trello.API, base
		s, err := trello.New(trello.Settings{Key: key, Token: token}, trello.Options{HTTP: r.Client()})
		if err != nil {
			t.Fatal(err)
		}
		return s, b
	case os.Getenv("TRELLO_CASSETTE") == "write":
		srv := httptest.NewServer(seeded())
		t.Cleanup(srv.Close)
		r := cassette.Record(t, file, "trello", "written from Trello's REST API reference (developer.atlassian.com/cloud/trello/rest) through trellosim", nil)
		r.From, r.To = srv.URL, base
		s, err := trello.New(trello.Settings{Key: "k", Token: "t"}, trello.Options{Base: srv.URL, HTTP: r.Client()})
		if err != nil {
			t.Fatal(err)
		}
		return s, board
	}
	s, err := trello.New(trello.Settings{Key: "k", Token: "t"}, trello.Options{Base: base, HTTP: cassette.Replay(t, file).Client()})
	if err != nil {
		t.Fatal(err)
	}
	return s, board
}

func ctxFor(op string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: tracker.Ref, Operation: op}
}

func caller(s adapter.Session) trackertest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		return s.Call(ctxFor(op), op, raw)
	}
}

// The contract suite against the cassette.
func TestContract(t *testing.T) {
	s, b := session(t, "testdata/contract.json")
	trackertest.Run(t, caller(s), trackertest.Options{Board: b, KeyPrefix: "suite"})
}

func live(t *testing.T, sim *trellosim.Server, token string) *trello.Session {
	t.Helper()
	srv := httptest.NewServer(sim)
	t.Cleanup(srv.Close)
	s, err := trello.New(trello.Settings{Key: "k", Token: token}, trello.Options{Base: srv.URL, Poll: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The suite again on the simulator with Watch running, so the events
// carry the cards' versions; and a change made in Trello is reported.
func TestWatch(t *testing.T) {
	sim := seeded()
	s := live(t, sim, "t")
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var events []adapter.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		c := ctxFor("")
		c.Context = ctx
		_ = s.Watch(c, json.RawMessage(`{"`+board+`":""}`), func(e adapter.Event) error {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
			return nil
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	got := func() []adapter.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]adapter.Event(nil), events...)
	}
	trackertest.Run(t, caller(s), trackertest.Options{Board: board, KeyPrefix: "watch", Events: got})

	card := sim.Cards(board)[0]
	v := sim.Change(card.ID, map[string]any{"idList": sim.Cards(board)[1].IDList, "name": "Moved in Trello"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, e := range got() {
			var p struct {
				Card, Change, Version string
				Fields                []string
			}
			_ = json.Unmarshal(e.Payload, &p)
			if p.Card == card.ID && p.Version == v {
				if p.Change != tracker.ChangeMoved || len(p.Fields) != 2 || e.Cursor == nil {
					t.Fatalf("the move's event: %s %s", e.Payload, e.Cursor)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no event for the change in Trello at %s", v)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Trello's errors as connector errors, and the checklist kept as the
// card's first checklist.
func TestErrorsAndChecklist(t *testing.T) {
	sim := seeded()
	call := caller(live(t, sim, "t"))
	code := func(err error) string {
		if ce := connectors.AsError(err); ce != nil {
			return ce.Code
		}
		return ""
	}
	if _, err := caller(live(t, sim, "bad"))("check", map[string]any{}); code(err) != connectors.CodeAuth {
		t.Errorf("a bad token = %v, want auth", err)
	}
	if _, err := call("card.get", map[string]any{"id": "656400000000000000ffffff"}); code(err) != connectors.CodeNotFound {
		t.Errorf("an unknown card = %v, want notFound", err)
	}
	sim.Throttle(1)
	_, err := call("check", map[string]any{})
	if ce := connectors.AsError(err); ce == nil || ce.Code != connectors.CodeRateLimited || ce.RetryAfterMs != 10_000 || !ce.Retry {
		t.Errorf("a 429 = %#v, want rateLimited after 10 s", ce)
	}

	var b struct{ Lists []tracker.List }
	raw, err := call("board.get", map[string]any{"id": board})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &b)
	raw, err = call("card.create", map[string]any{"key": "k1", "board": board, "list": b.Lists[0].ID, "title": "With a checklist", "description": "Steps",
		"checklist": []map[string]any{{"text": "One", "done": true}, {"text": "Two", "done": false}}})
	if err != nil {
		t.Fatal(err)
	}
	var c tracker.Card
	_ = json.Unmarshal(raw, &c)
	if c.Key != "k1" || c.Description != "Steps" || len(c.Checklist) != 2 || !c.Checklist[0].Done || c.Checklist[1].Text != "Two" {
		t.Fatalf("the created card: %+v", c)
	}
	items := []map[string]any{{"id": c.Checklist[1].ID, "text": "Two", "done": true}, {"text": "Three", "done": false}}
	raw, err = call("card.update", map[string]any{"id": c.ID, "version": c.Version, "set": map[string]any{"checklist": items, "description": "Steps, changed"}})
	if err != nil {
		t.Fatal(err)
	}
	var u tracker.Card
	_ = json.Unmarshal(raw, &u)
	if len(u.Checklist) != 2 || u.Checklist[0].Text != "Two" || !u.Checklist[0].Done || u.Checklist[1].Text != "Three" || u.Key != "k1" || u.Description != "Steps, changed" {
		t.Fatalf("the updated card: %+v", u)
	}
	// another session (another process) finds the card by its key
	raw, err = caller(live(t, sim, "t"))("card.create", map[string]any{"key": "k1", "board": board, "list": b.Lists[0].ID, "title": "Again"})
	var again tracker.Card
	_ = json.Unmarshal(raw, &again)
	if err != nil || again.ID != c.ID {
		t.Fatalf("a create with the same key from another session: %+v, %v", again, err)
	}
}

func TestType(t *testing.T) {
	if ps := connectors.CheckType(trello.Type(), func(ref string) (connectors.Contract, bool) {
		return tracker.Contract(), ref == tracker.Ref
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}
