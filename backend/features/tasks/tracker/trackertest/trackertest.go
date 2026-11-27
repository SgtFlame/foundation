// Package trackertest is the suite every tasks.tracker@1 type passes
// (forge 30g): the Tasks test server here, each tracker adapter against
// recorded responses in 30i, 30j and 30o.
package trackertest

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
)

// Caller calls one tasks.tracker operation of the type under test.
type Caller func(op string, args any) (json.RawMessage, error)

// Options say what the suite may use: a board the connection sees, with
// at least two lists, and the prefix of the keys it creates with (unique
// per run against a real tracker). Events, when set, gives the events
// the type raised since the suite began (a Watch the caller runs); the
// suite then checks they carry the cards' versions.
type Options struct {
	Board     string
	KeyPrefix string
	Events    func() []adapter.Event
}

// Run runs the suite: create is keyed, list pages are complete, a stale
// card.update and card.move are conflicts, a move changes the list,
// comments are keyed, archive hides a card from the list, every result has
// the contract's required fields, and events carry versions.
func Run(t *testing.T, call Caller, o Options) {
	t.Helper()
	if o.KeyPrefix == "" {
		o.KeyPrefix = "suite-" + fmt.Sprint(time.Now().UnixNano())
	}
	must := func(op string, args any, into any) {
		t.Helper()
		raw, err := call(op, args)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if into != nil {
			if err := json.Unmarshal(raw, into); err != nil {
				t.Fatalf("%s: %v in %s", op, err, raw)
			}
		}
	}
	code := func(err error) string {
		var ce *connectors.Error
		if errors.As(err, &ce) {
			return ce.Code
		}
		return fmt.Sprint(err)
	}
	if _, err := call("check", map[string]any{}); err != nil {
		t.Fatalf("check: %v", err)
	}
	var boards struct{ Boards []tracker.Board }
	must("board.list", map[string]any{}, &boards)
	if !slices.ContainsFunc(boards.Boards, func(b tracker.Board) bool { return b.ID == o.Board }) {
		t.Fatalf("board.list has no %q: %+v", o.Board, boards.Boards)
	}
	var board struct {
		Board   tracker.Board
		Lists   []tracker.List
		Labels  []tracker.Label
		Members []tracker.Member
	}
	must("board.get", map[string]any{"id": o.Board}, &board)
	if board.Board.ID != o.Board || len(board.Lists) < 2 {
		t.Fatalf("board.get gave %+v, want the board with two lists or more", board)
	}
	for _, l := range board.Lists {
		if l.ID == "" || l.Name == "" {
			t.Fatalf("a list without an id or name: %+v", l)
		}
	}
	first, second := board.Lists[0].ID, board.Lists[1].ID
	if _, err := call("board.get", map[string]any{"id": "no-such-board-" + o.KeyPrefix}); code(err) != connectors.CodeNotFound {
		t.Errorf("board.get of no board = %v, want notFound", err)
	}

	// Create is keyed
	var a, b tracker.Card
	create := map[string]any{"key": o.KeyPrefix + "-1", "board": o.Board, "list": first, "title": "Suite card", "description": "Made by the contract suite"}
	must("card.create", create, &a)
	checkCard(t, "card.create", a)
	must("card.create", create, &b)
	if b.ID != a.ID {
		t.Fatalf("a second create with key %s made another card: %s then %s", create["key"], a.ID, b.ID)
	}
	made := []string{a.ID}
	for i := 2; i <= 7; i++ {
		var c tracker.Card
		must("card.create", map[string]any{"key": fmt.Sprintf("%s-%d", o.KeyPrefix, i), "board": o.Board, "list": first, "title": fmt.Sprintf("Suite card %d", i)}, &c)
		made = append(made, c.ID)
	}

	// The pages hold every card once
	seen := map[string]int{}
	cursor := ""
	for page := 0; ; page++ {
		if page > 10000 {
			t.Fatal("card.list never ended")
		}
		args := map[string]any{"board": o.Board, "limit": 3}
		if cursor != "" {
			args["cursor"] = cursor
		}
		var res struct {
			Cards  []tracker.Card
			Cursor string
		}
		must("card.list", args, &res)
		if len(res.Cards) > 3 {
			t.Fatalf("card.list gave %d cards for limit 3", len(res.Cards))
		}
		for _, c := range res.Cards {
			checkCard(t, "card.list", c)
			seen[c.ID]++
		}
		if res.Cursor == "" {
			break
		}
		cursor = res.Cursor
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("card.list gave %s %d times", id, n)
		}
	}
	for _, id := range made {
		if seen[id] != 1 {
			t.Fatalf("card.list never gave %s", id)
		}
	}

	// A change against the version shown; a stale one is a conflict
	var got, updated tracker.Card
	must("card.get", map[string]any{"id": a.ID}, &got)
	must("card.update", map[string]any{"id": a.ID, "version": got.Version, "set": map[string]any{"title": "Suite card, renamed"}}, &updated)
	if updated.Title != "Suite card, renamed" || updated.Version == got.Version || updated.Description != got.Description {
		t.Fatalf("card.update gave %+v (from %+v)", updated, got)
	}
	if _, err := call("card.update", map[string]any{"id": a.ID, "version": got.Version, "set": map[string]any{"title": "Lost"}}); code(err) != connectors.CodeConflict {
		t.Fatalf("a stale card.update = %v, want conflict", err)
	}
	if _, err := call("card.move", map[string]any{"id": a.ID, "list": second, "version": got.Version}); code(err) != connectors.CodeConflict {
		t.Fatalf("a stale card.move = %v, want conflict", err)
	}
	var moved tracker.Card
	must("card.move", map[string]any{"id": a.ID, "list": second, "version": updated.Version}, &moved)
	if moved.List != second || moved.Version == updated.Version {
		t.Fatalf("card.move gave %+v", moved)
	}
	if _, err := call("card.get", map[string]any{"id": "no-such-card-" + o.KeyPrefix}); code(err) != connectors.CodeNotFound {
		t.Errorf("card.get of no card = %v, want notFound", err)
	}

	// Comments are keyed
	var c1, c2 tracker.Comment
	comment := map[string]any{"key": o.KeyPrefix + "-c1", "card": a.ID, "text": "From the suite"}
	must("comment.create", comment, &c1)
	must("comment.create", comment, &c2)
	if c1.ID == "" || c2.ID != c1.ID || c1.Card != a.ID {
		t.Fatalf("comment.create gave %+v then %+v", c1, c2)
	}
	var comments struct{ Comments []tracker.Comment }
	must("comment.list", map[string]any{"card": a.ID}, &comments)
	if n := len(slices.DeleteFunc(comments.Comments, func(c tracker.Comment) bool { return c.ID != c1.ID })); n != 1 {
		t.Fatalf("comment.list has the comment %d times", n)
	}

	// Archiving hides a card from the list
	var archived tracker.Card
	must("card.archive", map[string]any{"id": made[1]}, &archived)
	if !archived.Archived {
		t.Fatalf("card.archive gave %+v", archived)
	}
	var page struct{ Cards []tracker.Card }
	must("card.list", map[string]any{"board": o.Board, "limit": 500}, &page)
	if slices.ContainsFunc(page.Cards, func(c tracker.Card) bool { return c.ID == made[1] }) {
		t.Fatal("an archived card is listed")
	}

	if o.Events == nil {
		return
	}
	var final tracker.Card
	must("card.get", map[string]any{"id": a.ID}, &final)
	deadline := time.Now().Add(5 * time.Second)
	for {
		last := ""
		for _, e := range o.Events() {
			var p struct{ Card, Version, Change string }
			_ = json.Unmarshal(e.Payload, &p)
			if p.Card != a.ID {
				continue
			}
			if p.Version == "" {
				t.Fatalf("%s for %s has no version: %s", e.Name, a.ID, e.Payload)
			}
			last = p.Version
		}
		if last == final.Version {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the last event for %s has version %q, the card %q", a.ID, last, final.Version)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func checkCard(t *testing.T, op string, c tracker.Card) {
	t.Helper()
	if c.ID == "" || c.Board == "" || c.List == "" || c.Version == "" || c.UpdatedAt == "" {
		t.Fatalf("%s gave a card without an id, board, list, version or updatedAt: %+v", op, c)
	}
	if _, err := time.Parse(time.RFC3339Nano, c.UpdatedAt); err != nil {
		t.Fatalf("%s gave updatedAt %q: %v", op, c.UpdatedAt, err)
	}
}
