// Package trackerfake is the Tasks test server (forge 30g): a builtin
// connector type, tasks.fake, that implements tasks.tracker@1 over boards
// and cards it keeps in memory per connection, with outside changes and
// failures injected through its own contract tasks.fake.control@1, so the
// two-way sync of 30h is built and tested with no outside account.
//
// Its state lives in the process that serves the type (a test server, not
// a tracker): give it one adapter instance.
package trackerfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
)

var log = logging.In("tasks").Tags("trackerfake")

// The test server's type and control contract.
const (
	TypeID     = "tasks.fake"
	ControlID  = "tasks.fake.control"
	ControlRef = "tasks.fake.control@1"
	// ControlCall is the live call that runs a control operation on a
	// connection, for the forge trackertest tool.
	ControlCall = "tasks.fake.control"
)

// Type is the test server's connector type.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{tracker.Ref, ControlRef}, Source: connectors.SourceBuiltin,
		Settings: json.RawMessage(`{"type":"object","properties":{}}`),
		Meta: connectors.Meta{Title: "Tasks test server", Description: "Pretend task tracker in memory: implements tasks.tracker@1 over boards and cards a test seeds, and lets tests edit cards behind the sync's back, fail or slow calls through tasks.fake.control@1. For tests and trying sync by hand; it talks to no outside tracker.", Tags: []string{"tasks", "tracker", "test"}},
	}
}

// ControlContract is tasks.fake.control@1.
func ControlContract() connectors.Contract {
	ok := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true"}},"required":["ok"]}`)
	ops := `"operation":{"type":"string","description":"The tasks.tracker operation, e.g. card.update"}`
	return connectors.Contract{
		ID: ControlID, Version: 1,
		Meta: connectors.Meta{Title: "Tasks test server control", Description: "Seeds the Tasks test server's boards, edits its cards as someone in the tracker would (raising card.changed), fails or slows its calls, and shows its state. For tests only.", Tags: []string{"tasks", "tracker", "test"}},
		Operations: []connectors.Operation{
			{
				ID: "seed", Title: "Seed a board", Description: "Makes a board (or adds to one) with its lists, labels and members, and cards: those given, then count more made up, spread over the lists. A seeded card raises no event.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "board":{"type":"object","description":"The board","properties":{
    "id":{"type":"string","description":"Its id"},
    "name":{"type":"string","description":"Its name"},
    "lists":{"type":"array","items":{"type":"string"},"description":"Its lists' names, in order; To do, Doing, Done when left out"},
    "labels":{"type":"array","items":{"type":"string"},"description":"Its labels' names"},
    "members":{"type":"array","items":{"type":"string"},"description":"Its members' names"}},"required":["id"]},
  "cards":{"type":"array","description":"Cards to make, each as card.create takes them (list and labels may be names)","items":{"type":"object","properties":{
    "title":{"type":"string","description":"Its title"},
    "list":{"type":"string","description":"Its list's id or name; the first when left out"},
    "description":{"type":"string","description":"Its description"},
    "due":{"type":"string","description":"When it is due"}},"required":["title"]}},
  "count":{"type":"integer","minimum":0,"maximum":10000,"description":"How many more cards to make up, titled Card 1, Card 2…"}},"required":["board"]}`),
				Result:   json.RawMessage(`{"type":"object","properties":{"board":{"type":"string","description":"The board's id"},"cards":{"type":"integer","description":"How many cards it now has"}},"required":["board","cards"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"board":{"id":"b1","name":"Website relaunch","lists":["To do","Doing","Review","Done"]},"count":500}`), Result: json.RawMessage(`{"board":"b1","cards":500}`)}},
			},
			{
				ID: "change", Title: "Change a card outside", Description: "Changes a card as someone in the tracker would: sets fields (those card.update takes, plus list and position to move it and archived), gives it a new version and raises card.changed.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "card":{"type":"string","description":"The card's id"},
  "set":{"type":"object","description":"Field → new value: title, description, assignees, due, start, labels, checklist, list, position, archived"},
  "comment":{"type":"string","description":"A comment to add as the first member, raising comment.created"}},"required":["card"]}`),
				Result:   json.RawMessage(`{"type":"object","properties":{"version":{"type":"string","description":"The card's new version"}},"required":["version"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"card":"c1","set":{"title":"Renamed in the tracker"}}`), Result: json.RawMessage(`{"version":"4"}`)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeInvalid},
			},
			{
				ID: "fail", Title: "Fail the next calls", Description: "Makes the next times calls of a tasks.tracker operation fail with the error kind given (rateLimited and unavailable are retried by jobs).",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{` + ops + `,
  "times":{"type":"integer","minimum":1,"description":"How many calls fail; once when left out"},
  "kind":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"description":"The error code; unavailable when left out"}},"required":["operation"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"operation":"card.update","times":2,"kind":"rateLimited"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors: []string{connectors.CodeInvalid},
			},
			{
				ID: "slow", Title: "Slow the calls", Description: "Makes every call of a tasks.tracker operation wait ms milliseconds first; 0 stops it.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{` + ops + `,
  "ms":{"type":"integer","minimum":0,"description":"Milliseconds each call waits"}},"required":["operation","ms"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"operation":"card.list","ms":200}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors: []string{connectors.CodeInvalid},
			},
			{
				ID: "state", Title: "Show the state", Description: "Every board with its card count, the events raised so far, and the faults still to happen.",
				Effect: connectors.EffectRead,
				Args:   json.RawMessage(`{"type":"object","properties":{}}`),
				Result: json.RawMessage(`{"type":"object","properties":{
  "boards":{"type":"array","description":"The boards","items":{"type":"object","properties":{"id":{"type":"string","description":"The board's id"},"name":{"type":"string","description":"Its name"},"cards":{"type":"integer","description":"Its cards, archived ones too"}},"required":["id","cards"]}},
  "events":{"type":"integer","description":"Events raised so far"},
  "faults":{"type":"array","description":"Faults still to happen","items":{"type":"object","properties":{"operation":{"type":"string","description":"The operation"},"times":{"type":"integer","description":"Calls left to fail"},"kind":{"type":"string","description":"The error code"},"slowMs":{"type":"integer","description":"Milliseconds each call waits"}},"required":["operation"]}}},"required":["boards","events","faults"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"boards":[{"id":"b1","name":"Website relaunch","cards":500}],"events":0,"faults":[]}`)}},
			},
		},
	}
}

// Dispatcher calls a connector operation: connectors.Dispatcher.
type Dispatcher interface {
	Call(args features.ServiceArgs, c connectors.Call) (json.RawMessage, *connectors.Error)
}

// Contribute registers the test server's type and control contract, and
// the tasks.fake.control live call, which runs a control operation on a
// connection as the caller (the dispatcher checks they may use it).
func Contribute(reg *extensions.Registry, dispatcher func() Dispatcher) {
	connectors.ContributeBuiltins(reg, "tasks", []connectors.Contract{ControlContract()}, []connectors.Type{Type()})
	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: ControlCall, Source: "tasks"}, Title: "Control the Tasks test server",
		Description: "Runs one tasks.fake.control@1 operation (seed, change, fail, slow, state) on a Tasks test server connection {connection, operation, args}. Ends with one result event, the operation's result, or fails with its error.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var a struct {
				Connection, Operation string
				Args                  json.RawMessage
			}
			if err := json.Unmarshal(c.Args, &a); err != nil || a.Connection == "" || a.Operation == "" {
				return c.Fail(http.StatusBadRequest, log.Code("invalid_args").Errorf("%s needs a connection and an operation", ControlCall))
			}
			if len(a.Args) == 0 {
				a.Args = json.RawMessage(`{}`)
			}
			d := dispatcher()
			if d == nil {
				return c.Fail(http.StatusServiceUnavailable, log.Code("no_connectors").Errorf("connectors are not running"))
			}
			res, e := d.Call(c.ServiceArgs, connectors.Call{Contract: ControlRef, Operation: a.Operation, Connection: a.Connection, Args: a.Args})
			if e != nil {
				return c.Fail(statusOf(e), log.Code(e.Code).Errorf("%s", e.Message))
			}
			c.Emit("result", res)
			return nil
		},
	})
}

func statusOf(e *connectors.Error) uint16 {
	switch e.Code {
	case connectors.CodeNotFound:
		return http.StatusNotFound
	case connectors.CodeInvalid:
		return http.StatusBadRequest
	case connectors.CodeAuth:
		return http.StatusForbidden
	case connectors.CodeConflict:
		return http.StatusConflict
	}
	return http.StatusBadGateway
}

// Server holds every connection's boards in this process.
type Server struct {
	mu    sync.Mutex
	conns map[string]*account
	now   func() time.Time
}

// New makes an empty server.
func New() *Server { return &Server{conns: map[string]*account{}, now: time.Now} }

// Default is this process's server.
var Default = New()

type fault struct {
	times  int
	kind   string
	slowMs int
}

type event struct {
	seq     int
	name    string
	payload map[string]any
}

type board struct {
	tracker.Board
	lists   []tracker.List
	labels  []tracker.Label
	members []tracker.Member
}

type account struct {
	boards   map[string]*board
	cards    map[string]*tracker.Card
	keys     map[string]string // our key → card id
	comments map[string][]tracker.Comment
	ckeys    map[string]tracker.Comment
	faults   map[string]*fault
	events   []event
	next     int
	wake     chan struct{}
}

func (s *Server) account(tenant uint64, conn string) *account {
	k := strconv.FormatUint(tenant, 10) + "." + conn
	a := s.conns[k]
	if a == nil {
		a = &account{boards: map[string]*board{}, cards: map[string]*tracker.Card{}, keys: map[string]string{}, comments: map[string][]tracker.Comment{},
			ckeys: map[string]tracker.Comment{}, faults: map[string]*fault{}, wake: make(chan struct{})}
		s.conns[k] = a
	}
	return a
}

func (a *account) id(prefix string) string {
	a.next++
	return prefix + strconv.Itoa(a.next)
}

// emit logs an event and wakes the watches; under s.mu.
func (a *account) emit(name string, payload map[string]any) {
	seq := len(a.events) + 1
	payload["id"] = "evt_" + strconv.Itoa(seq)
	a.events = append(a.events, event{seq: seq, name: name, payload: payload})
	close(a.wake)
	a.wake = make(chan struct{})
}

// touch gives a card a new version and updatedAt; under s.mu.
func (s *Server) touch(c *tracker.Card) {
	v, _ := strconv.Atoi(c.Version)
	c.Version = strconv.Itoa(v + 1)
	c.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
}

func (s *Server) changed(a *account, c *tracker.Card, change string, fields []string) {
	a.emit("card.changed", map[string]any{"card": c.ID, "board": c.Board, "change": change, "version": c.Version, "updatedAt": c.UpdatedAt, "fields": fields})
}

// Impl is the adapter implementation of the test server's type.
func (s *Server) Impl() adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		return &session{s: s, tenant: ctx.Tenant(), conn: conn.ID}, nil
	}}
}

// Options are adapter options serving only the test server, for tests.
func (s *Server) Options(name string) adapter.Options {
	return adapter.Options{Name: name, Types: []connectors.Type{Type()}, Contracts: []connectors.Contract{tracker.Contract(), ControlContract()}, Impls: []adapter.Impl{s.Impl()}}
}

type session struct {
	s      *Server
	tenant uint64
	conn   string
}

func (x *session) Close() error { return nil }

// Watch reports card.changed and comment.created from the cursor, the
// last event's sequence.
func (x *session) Watch(ctx adapter.Context, cursor json.RawMessage, emit func(adapter.Event) error) error {
	from := 0
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &from)
	}
	for {
		x.s.mu.Lock()
		a := x.s.account(x.tenant, x.conn)
		pending := append([]event(nil), a.events[min(from, len(a.events)):]...)
		wake := a.wake
		x.s.mu.Unlock()
		for _, e := range pending {
			payload, _ := json.Marshal(e.payload)
			c, _ := json.Marshal(e.seq)
			id := fmt.Sprintf("%s-%d", x.conn, e.seq)
			if err := emit(adapter.Event{Contract: tracker.Ref, Name: e.name, ID: id, Payload: payload, Cursor: c}); err != nil {
				return err
			}
			from = e.seq
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (x *session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	if ctx.Contract == ControlRef {
		return x.control(op, args)
	}
	x.s.mu.Lock()
	a := x.s.account(x.tenant, x.conn)
	var slow int
	var fail *connectors.Error
	if f := a.faults[op]; f != nil {
		slow = f.slowMs
		if f.times > 0 {
			f.times--
			fail = connectors.Errorf(f.kind, "the test server failed %s as asked", op)
		}
		if f.times == 0 && f.slowMs == 0 {
			delete(a.faults, op)
		}
	}
	x.s.mu.Unlock()
	if slow > 0 {
		select {
		case <-ctx.Done():
			return nil, connectors.Errorf(connectors.CodeUnavailable, "cancelled")
		case <-time.After(time.Duration(slow) * time.Millisecond):
		}
	}
	if fail != nil {
		return nil, fail
	}
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return x.s.call(x.s.account(x.tenant, x.conn), op, args)
}

type cardArgs struct {
	ID, Key, Board, List, Version, Cursor string
	Position                              *float64
	Limit                                 int
	Archived                              bool
	Card, Text                            string
	Set                                   map[string]json.RawMessage
}

func (s *Server) call(a *account, op string, raw json.RawMessage) (json.RawMessage, error) {
	var in cardArgs
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, connectors.Errorf(connectors.CodeInvalid, "%v", err)
	}
	switch op {
	case "check":
		return json.RawMessage(`{"ok":true}`), nil
	case "board.list":
		boards := []tracker.Board{}
		for _, b := range a.sortedBoards() {
			if in.Archived || !b.Archived {
				boards = append(boards, b.Board)
			}
		}
		return json.Marshal(map[string]any{"boards": boards})
	case "board.get":
		b := a.boards[in.ID]
		if b == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no board %q", in.ID)
		}
		return json.Marshal(map[string]any{"board": b.Board, "lists": b.lists, "labels": b.labels, "members": b.members})
	case "member.list":
		b := a.boards[in.Board]
		if b == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no board %q", in.Board)
		}
		return json.Marshal(map[string]any{"members": b.members})
	case "card.list":
		if a.boards[in.Board] == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no board %q", in.Board)
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 100
		}
		var ids []string
		for id, c := range a.cards {
			if c.Board == in.Board && (in.Archived || !c.Archived) && cardOrder(id, in.Cursor) > 0 {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return cardOrder(ids[i], ids[j]) < 0 })
		next := ""
		if len(ids) > limit {
			ids = ids[:limit]
			next = ids[limit-1]
		}
		cards := []tracker.Card{}
		for _, id := range ids {
			cards = append(cards, *a.cards[id])
		}
		return json.Marshal(map[string]any{"cards": cards, "cursor": next})
	case "card.get":
		c := a.cards[in.ID]
		if c == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no card %q", in.ID)
		}
		return json.Marshal(c)
	case "card.create":
		if id, ok := a.keys[in.Key]; ok && in.Key != "" {
			return json.Marshal(a.cards[id])
		}
		b := a.boards[in.Board]
		if b == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no board %q", in.Board)
		}
		if !slices.ContainsFunc(b.lists, func(l tracker.List) bool { return l.ID == in.List }) {
			return nil, connectors.Errorf(connectors.CodeInvalid, "board %s has no list %q", in.Board, in.List)
		}
		c := &tracker.Card{ID: a.id("c"), Key: in.Key, Board: in.Board, List: in.List, Assignees: []string{}, Labels: []string{}, Checklist: []tracker.CheckItem{}}
		c.Position = a.endOf(in.List) + 1024
		if in.Position != nil {
			c.Position = *in.Position
		}
		var set map[string]json.RawMessage
		_ = json.Unmarshal(raw, &set)
		if _, err := s.apply(a, c, set, tracker.CardFields); err != nil {
			return nil, err
		}
		if c.Title == "" {
			return nil, connectors.Errorf(connectors.CodeInvalid, "a card needs a title")
		}
		s.touch(c)
		a.cards[c.ID] = c
		if in.Key != "" {
			a.keys[in.Key] = c.ID
		}
		s.changed(a, c, tracker.ChangeCreated, nil)
		return json.Marshal(c)
	case "card.update", "card.move", "card.archive":
		c := a.cards[in.ID]
		if c == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no card %q", in.ID)
		}
		if in.Version != "" && in.Version != c.Version {
			return nil, connectors.Errorf(connectors.CodeConflict, "card %s is at version %s, not %s", c.ID, c.Version, in.Version)
		}
		change, set, allowed := tracker.ChangeUpdated, in.Set, tracker.CardFields
		switch op {
		case "card.move":
			change, allowed = tracker.ChangeMoved, []string{"list", "position"}
			set = map[string]json.RawMessage{"list": mustJSON(in.List)}
			if in.Position != nil {
				set["position"] = mustJSON(*in.Position)
			} else if in.List != c.List {
				set["position"] = mustJSON(a.endOf(in.List) + 1024)
			}
		case "card.archive":
			if c.Archived {
				return json.Marshal(c)
			}
			change, allowed, set = tracker.ChangeArchived, []string{"archived"}, map[string]json.RawMessage{"archived": json.RawMessage("true")}
		}
		fields, err := s.apply(a, c, set, allowed)
		if err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			s.touch(c)
			s.changed(a, c, change, fields)
		}
		return json.Marshal(c)
	case "comment.list":
		if a.cards[in.Card] == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no card %q", in.Card)
		}
		comments := append([]tracker.Comment{}, a.comments[in.Card]...)
		return json.Marshal(map[string]any{"comments": comments})
	case "comment.create":
		if cm, ok := a.ckeys[in.Key]; ok && in.Key != "" {
			return json.Marshal(cm)
		}
		c := a.cards[in.Card]
		if c == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no card %q", in.Card)
		}
		cm := s.comment(a, c, in.Key, "", in.Text)
		return json.Marshal(cm)
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "no operation %q", op)
}

// comment adds a comment, raising comment.created; under s.mu.
func (s *Server) comment(a *account, c *tracker.Card, key, author, text string) tracker.Comment {
	if author == "" {
		if b := a.boards[c.Board]; b != nil && len(b.members) > 0 {
			author = b.members[0].ID
		}
	}
	cm := tracker.Comment{ID: a.id("cm"), Key: key, Card: c.ID, Author: author, Text: text, CreatedAt: s.now().UTC().Format(time.RFC3339Nano)}
	a.comments[c.ID] = append(a.comments[c.ID], cm)
	if key != "" {
		a.ckeys[key] = cm
	}
	s.touch(c)
	a.emit("comment.created", map[string]any{"card": c.ID, "board": c.Board, "comment": cm.ID, "version": c.Version, "createdAt": cm.CreatedAt})
	return cm
}

// apply sets fields on a card, giving those that changed; under s.mu.
func (s *Server) apply(a *account, c *tracker.Card, set map[string]json.RawMessage, allowed []string) ([]string, error) {
	var changed []string
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !slices.Contains(allowed, k) {
			continue
		}
		v := set[k]
		before, _ := json.Marshal(c)
		var err error
		switch k {
		case "title":
			err = json.Unmarshal(v, &c.Title)
		case "description":
			err = json.Unmarshal(v, &c.Description)
		case "due":
			err = json.Unmarshal(v, &c.Due)
		case "start":
			err = json.Unmarshal(v, &c.Start)
		case "assignees":
			err = json.Unmarshal(v, &c.Assignees)
		case "labels":
			err = json.Unmarshal(v, &c.Labels)
		case "checklist":
			err = json.Unmarshal(v, &c.Checklist)
			for i := range c.Checklist {
				if c.Checklist[i].ID == "" {
					c.Checklist[i].ID = a.id("ci")
				}
			}
		case "list":
			var l string
			if err = json.Unmarshal(v, &l); err == nil {
				b := a.boards[c.Board]
				if b == nil || !slices.ContainsFunc(b.lists, func(x tracker.List) bool { return x.ID == l }) {
					return nil, connectors.Errorf(connectors.CodeInvalid, "board %s has no list %q", c.Board, l)
				}
				c.List = l
			}
		case "position":
			err = json.Unmarshal(v, &c.Position)
		case "archived":
			err = json.Unmarshal(v, &c.Archived)
		}
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "%s: %v", k, err)
		}
		if after, _ := json.Marshal(c); string(after) != string(before) {
			changed = append(changed, k)
		}
	}
	if c.Assignees == nil {
		c.Assignees = []string{}
	}
	if c.Labels == nil {
		c.Labels = []string{}
	}
	if c.Checklist == nil {
		c.Checklist = []tracker.CheckItem{}
	}
	return changed, nil
}

// endOf is the largest position in a list; under s.mu.
func (a *account) endOf(list string) float64 {
	end := 0.0
	for _, c := range a.cards {
		if c.List == list && c.Position > end {
			end = c.Position
		}
	}
	return end
}

func (a *account) sortedBoards() []*board {
	out := make([]*board, 0, len(a.boards))
	for _, b := range a.boards {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// cardOrder orders card ids as made: c2 before c10. An empty id (no
// cursor) is before every card.
func cardOrder(a, b string) int {
	if b == "" {
		return 1
	}
	if a == "" {
		return -1
	}
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func (x *session) control(op string, raw json.RawMessage) (json.RawMessage, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	a := x.s.account(x.tenant, x.conn)
	switch op {
	case "seed":
		return x.s.seed(a, raw)
	case "change":
		var in struct {
			Card, Comment string
			Set           map[string]json.RawMessage
		}
		_ = json.Unmarshal(raw, &in)
		c := a.cards[in.Card]
		if c == nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no card %q", in.Card)
		}
		allowed := append(slices.Clone(tracker.CardFields), "list", "position", "archived")
		for k := range in.Set {
			if !slices.Contains(allowed, k) {
				return nil, connectors.Errorf(connectors.CodeInvalid, "a card has no field %q to change: use %s", k, strings.Join(allowed, ", "))
			}
		}
		fields, err := x.s.apply(a, c, in.Set, allowed)
		if err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			x.s.touch(c)
			change := tracker.ChangeUpdated
			switch {
			case slices.Contains(fields, "archived") && c.Archived:
				change = tracker.ChangeArchived
			case slices.Contains(fields, "list") || slices.Contains(fields, "position"):
				change = tracker.ChangeMoved
			}
			x.s.changed(a, c, change, fields)
		}
		if in.Comment != "" {
			x.s.comment(a, c, "", "", in.Comment)
		}
		return json.Marshal(map[string]any{"version": c.Version})
	case "fail", "slow":
		var in struct {
			Operation, Kind string
			Times, Ms       int
		}
		_ = json.Unmarshal(raw, &in)
		if _, ok := tracker.Contract().Operation(in.Operation); !ok {
			return nil, connectors.Errorf(connectors.CodeInvalid, "tasks.tracker has no operation %q", in.Operation)
		}
		f := a.faults[in.Operation]
		if f == nil {
			f = &fault{}
			a.faults[in.Operation] = f
		}
		if op == "fail" {
			f.times = max(in.Times, 1)
			f.kind = in.Kind
			if f.kind == "" {
				f.kind = connectors.CodeUnavailable
			}
		} else {
			f.slowMs = in.Ms
		}
		if f.times == 0 && f.slowMs == 0 {
			delete(a.faults, in.Operation)
		}
		return json.RawMessage(`{"ok":true}`), nil
	case "state":
		boards := []map[string]any{}
		for _, b := range a.sortedBoards() {
			n := 0
			for _, c := range a.cards {
				if c.Board == b.ID {
					n++
				}
			}
			boards = append(boards, map[string]any{"id": b.ID, "name": b.Name, "cards": n})
		}
		faults := []map[string]any{}
		ops := make([]string, 0, len(a.faults))
		for o := range a.faults {
			ops = append(ops, o)
		}
		sort.Strings(ops)
		for _, o := range ops {
			f := a.faults[o]
			faults = append(faults, map[string]any{"operation": o, "times": f.times, "kind": f.kind, "slowMs": f.slowMs})
		}
		return json.Marshal(map[string]any{"boards": boards, "events": len(a.events), "faults": faults})
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "no operation %q", op)
}

func (s *Server) seed(a *account, raw json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Board struct {
			ID, Name               string
			Lists, Labels, Members []string
		}
		Cards []struct{ Title, List, Description, Due string }
		Count int
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Board.ID == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "seed needs a board with an id")
	}
	b := a.boards[in.Board.ID]
	if b == nil {
		name := in.Board.Name
		if name == "" {
			name = in.Board.ID
		}
		b = &board{Board: tracker.Board{ID: in.Board.ID, Name: name, URL: "https://tasks.fake/b/" + in.Board.ID}}
		lists := in.Board.Lists
		if len(lists) == 0 {
			lists = []string{"To do", "Doing", "Done"}
		}
		for i, l := range lists {
			b.lists = append(b.lists, tracker.List{ID: b.ID + "-l" + strconv.Itoa(i+1), Name: l, Position: float64(i + 1)})
		}
		b.labels, b.members = []tracker.Label{}, []tracker.Member{}
		for i, l := range in.Board.Labels {
			b.labels = append(b.labels, tracker.Label{ID: b.ID + "-lb" + strconv.Itoa(i+1), Name: l})
		}
		for i, m := range in.Board.Members {
			b.members = append(b.members, tracker.Member{ID: b.ID + "-m" + strconv.Itoa(i+1), Name: m, Username: strings.ToLower(strings.Fields(m + " x")[0])})
		}
		a.boards[b.ID] = b
	}
	listOf := func(s string, i int) string {
		for _, l := range b.lists {
			if l.ID == s || strings.EqualFold(l.Name, s) {
				return l.ID
			}
		}
		return b.lists[i%len(b.lists)].ID
	}
	add := func(title, list, desc, due string) {
		c := &tracker.Card{ID: a.id("c"), Board: b.ID, List: list, Title: title, Description: desc, Due: due,
			Assignees: []string{}, Labels: []string{}, Checklist: []tracker.CheckItem{}, Position: a.endOf(list) + 1024}
		s.touch(c)
		a.cards[c.ID] = c
	}
	for _, c := range in.Cards {
		add(c.Title, listOf(c.List, 0), c.Description, c.Due)
	}
	for i := range in.Count {
		add(fmt.Sprintf("Card %d", i+1), listOf("", i), "", "")
	}
	n := 0
	for _, c := range a.cards {
		if c.Board == b.ID {
			n++
		}
	}
	return json.Marshal(map[string]any{"board": b.ID, "cards": n})
}
