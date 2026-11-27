// Package caltest is the Calendar test server (forge 28c): a builtin
// connector type, caltest.calendar, implementing calendar.events@1 over
// calendars it keeps in memory per connection, scripted through its own
// contract caltest.control@1: outside changes (add, move, remove), an
// invitee who answers on cue, failures, an expired cursor and dropped
// notifications, so every calendar part is tested without an outside
// account (question 28.6, infratest's form).
//
// Its state lives in the process that serves the type (a test server, not
// a provider): give it one adapter instance.
package caltest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// TypeID is the test server's connector type.
const (
	TypeID     = "caltest.calendar"
	ControlID  = "caltest.control"
	ControlRef = "caltest.control@1"
)

const settings = `{"type":"object","properties":{
  "email":{"type":"string","title":"Email","description":"The account's email: its own attendee entry, and how other connections' free/busy finds it"},
  "name":{"type":"string","title":"Name","description":"The account's name, the primary calendar's name"},
  "zone":{"type":"string","title":"Time zone","description":"The calendars' zone, e.g. Europe/London"}}}`

// Type is the test server's connector type.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{calendar.EventsRef, ControlRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(settings),
		Meta: connectors.Meta{Title: "Calendar test server", Description: "Pretend outside calendars in memory: implements calendar.events@1 with a primary and a team calendar per connection, and lets tests make outside changes, fail calls, expire the sync cursor, drop notifications and have invitees answer through caltest.control@1. For tests and trying calendars by hand; it reaches no real calendar.", Tags: []string{"calendar", "test"}},
	}
}

// ControlContract is caltest.control@1.
func ControlContract() connectors.Contract {
	ok := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true"}},"required":["ok"]}`)
	evResult := json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The event's id"},"etag":{"type":"string","description":"Its etag"}},"required":["id"]}`)
	return connectors.Contract{
		ID: ControlID, Version: 1,
		Meta: connectors.Meta{Title: "Calendar test server control", Description: "Makes the Calendar test server act like the outside world: someone adding, moving or removing an event at the service, an invitee answering, calls failing, the sync cursor expiring and notifications getting lost; and shows its calendars, events and pending faults. For tests only.", Tags: []string{"calendar", "test"}},
		Operations: []connectors.Operation{
			{
				ID: "add", Title: "Add an event outside", Description: "Adds an event as if someone made it at the service, raising eventChanged.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "calendar":{"type":"string","description":"The calendar's id; primary when left out"},
  "event":` + calendar.EventSchema + `},"required":["event"]}`),
				Result:   evResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"event":{"title":"Dentist","start":{"local":"2026-12-02T15:00:00","zone":"Europe/London"},"end":{"local":"2026-12-02T16:00:00","zone":"Europe/London"}}}`), Result: json.RawMessage(`{"id":"ct-1","etag":"1"}`)}},
			},
			{
				ID: "move", Title: "Move an event outside", Description: "Moves an event to new times as if someone did at the service, raising eventChanged and changing its etag.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "calendar":{"type":"string","description":"The calendar's id; primary when left out"},
  "id":{"type":"string","description":"The event's id"},
  "start":{"type":"object","description":"The new start, a zoned time or date"},
  "end":{"type":"object","description":"The new end"},
  "title":{"type":"string","description":"A new title, when given"}},"required":["id","start","end"]}`),
				Result:   evResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"ct-1","start":{"local":"2026-12-02T16:00:00","zone":"Europe/London"},"end":{"local":"2026-12-02T17:00:00","zone":"Europe/London"}}`), Result: json.RawMessage(`{"id":"ct-1","etag":"2"}`)}},
				Errors:   []string{connectors.CodeNotFound},
			},
			{
				ID: "remove", Title: "Remove an event outside", Description: "Deletes an event as if someone did at the service, raising eventRemoved.",
				Effect: connectors.EffectWrite, Destructive: true,
				Args:     json.RawMessage(`{"type":"object","properties":{"calendar":{"type":"string","description":"The calendar's id; primary when left out"},"id":{"type":"string","description":"The event's id"}},"required":["id"]}`),
				Result:   ok,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"ct-1"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeNotFound},
			},
			{
				ID: "answer", Title: "An invitee answers", Description: "An attendee answers an event's invitation at the service, as an outside invitee would on cue, raising eventChanged.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "calendar":{"type":"string","description":"The calendar's id; primary when left out"},
  "id":{"type":"string","description":"The event's id"},
  "email":{"type":"string","description":"The attendee's email"},
  "response":{"type":"string","enum":["accepted","tentative","declined"],"description":"Their answer"}},"required":["id","email","response"]}`),
				Result:   evResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"ct-1","email":"dana@example.com","response":"accepted"}`), Result: json.RawMessage(`{"id":"ct-1","etag":"3"}`)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeInvalid},
			},
			{
				ID: "fail", Title: "Fail the next calls", Description: "Makes the next times calls of a calendar.events operation fail with the error kind given (rateLimited and unavailable are retried by jobs).",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "operation":{"type":"string","description":"The calendar.events operation, e.g. event.update"},
  "times":{"type":"integer","minimum":1,"description":"How many calls fail; once when left out"},
  "kind":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"description":"The error code; unavailable when left out"}},"required":["operation"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"operation":"event.update","times":2,"kind":"unavailable"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors: []string{connectors.CodeInvalid},
			},
			{
				ID: "expireCursor", Title: "Expire the cursor", Description: "Makes every sync cursor given out so far too old, so the next event.list with one is refused with cursor_expired.",
				Effect: connectors.EffectWrite, Args: json.RawMessage(`{"type":"object","properties":{}}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`)}},
			},
			{
				ID: "dropNotifications", Title: "Drop notifications", Description: "Loses the next n change notifications (eventChanged, eventRemoved) as a flaky push would, while the changes themselves still happen, to prove a missed notice is caught up by the next delta.",
				Effect: connectors.EffectWrite,
				Args:   json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","minimum":1,"description":"How many notices to lose"}},"required":["n"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"n":1}`), Result: json.RawMessage(`{"ok":true}`)}},
			},
			{
				ID: "state", Title: "Show the state", Description: "The connection's calendars with their events, the cursor, and the faults still to happen.",
				Effect: connectors.EffectRead, Args: json.RawMessage(`{"type":"object","properties":{}}`),
				Result: json.RawMessage(`{"type":"object","properties":{
  "calendars":{"type":"array","description":"The calendars with their events","items":{"type":"object","properties":{"id":{"type":"string","description":"Its id"},"name":{"type":"string","description":"Its name"},"events":{"type":"array","items":` + calendar.EventSchema + `,"description":"Its events"}},"required":["id","events"]}},
  "cursor":{"type":"string","description":"The current cursor"},
  "faults":{"type":"array","description":"Faults still to happen","items":{"type":"object","properties":{"operation":{"type":"string","description":"The operation"},"times":{"type":"integer","description":"Calls left to fail"},"kind":{"type":"string","description":"The error code"}},"required":["operation"]}},
  "drop":{"type":"integer","description":"Notices still to lose"}},"required":["calendars","faults"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"calendars":[{"id":"primary","name":"Tony","events":[]}],"cursor":"0","faults":[],"drop":0}`)}},
			},
		},
	}
}

// Contribute registers the test server's type and control contract.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, TypeID, []connectors.Contract{ControlContract()}, []connectors.Type{Type()})
}

// Server holds every connection's calendars in this process.
type Server struct {
	mu    sync.Mutex
	conns map[string]*account
}

// New makes an empty server.
func New() *Server { return &Server{conns: map[string]*account{}} }

// Default is this process's server.
var Default = New()

type stored struct {
	ev   calendar.Event
	cal  string
	gone bool
}

type change struct {
	seq     int
	cal     string
	id      string
	removed bool
	notice  bool // whether a notification is raised
}

type fault struct {
	times int
	kind  string
}

type account struct {
	email, name, zone string
	calendars         []calendar.Calendar
	events            map[string]*stored // by id
	keys              map[string]string  // create key → id
	changes           []change
	expiredBefore     int
	drop              int
	faults            map[string]*fault
	next              int
	wake              chan struct{}
}

func (s *Server) account(tenant uint64, conn string) *account {
	k := strconv.FormatUint(tenant, 10) + "." + conn
	a := s.conns[k]
	if a == nil {
		a = &account{events: map[string]*stored{}, keys: map[string]string{}, faults: map[string]*fault{}, wake: make(chan struct{}), zone: "UTC"}
		s.conns[k] = a
	}
	return a
}

func (a *account) setup(email, name, zone string) {
	if email != "" {
		a.email = strings.ToLower(email)
	}
	if name != "" {
		a.name = name
	}
	if zone != "" {
		a.zone = zone
	}
	if a.name == "" {
		a.name = "Primary"
	}
	if len(a.calendars) == 0 {
		a.calendars = []calendar.Calendar{
			{ID: "primary", Name: a.name, Color: "#3f51b5", Zone: a.zone, Access: "owner", Primary: true},
			{ID: "team", Name: "Team", Color: "#009688", Zone: a.zone, Access: "writer"},
		}
	}
}

func (a *account) cursor() int { return len(a.changes) }

// log records a change and wakes the watches; under s.mu.
func (a *account) log(cal, id string, removed bool) {
	notice := true
	if a.drop > 0 {
		a.drop--
		notice = false
	}
	a.changes = append(a.changes, change{seq: len(a.changes) + 1, cal: cal, id: id, removed: removed, notice: notice})
	close(a.wake)
	a.wake = make(chan struct{})
}

func (a *account) hasCalendar(id string) bool {
	for _, c := range a.calendars {
		if c.ID == id {
			return true
		}
	}
	return false
}

// Impl is the adapter implementation of the test server's type.
func (s *Server) Impl() adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st struct{ Email, Name, Zone string }
		_ = json.Unmarshal(conn.Settings, &st)
		s.mu.Lock()
		s.account(ctx.Tenant(), conn.ID).setup(st.Email, st.Name, st.Zone)
		s.mu.Unlock()
		return &session{s: s, tenant: ctx.Tenant(), conn: conn.ID}, nil
	}}
}

// Options are adapter options serving only the test server, for tests.
func (s *Server) Options(name string) adapter.Options {
	return adapter.Options{Name: name, Types: []connectors.Type{Type()}, Contracts: []connectors.Contract{calendar.EventsContract(), ControlContract()}, Impls: []adapter.Impl{s.Impl()}}
}

type session struct {
	s      *Server
	tenant uint64
	conn   string
}

func (x *session) Close() error { return nil }

// Watch raises eventChanged and eventRemoved for each change after the
// cursor (notices dropNotifications lost are skipped, their changes
// kept for event.list).
func (x *session) Watch(ctx adapter.Context, cursor json.RawMessage, emit func(adapter.Event) error) error {
	from := 0
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &from)
	}
	for {
		x.s.mu.Lock()
		a := x.s.account(x.tenant, x.conn)
		pending := append([]change(nil), a.changes[min(from, len(a.changes)):]...)
		var etags []string
		for _, c := range pending {
			e := ""
			if st := a.events[c.id]; st != nil {
				e = st.ev.Etag
			}
			etags = append(etags, e)
		}
		wake := a.wake
		x.s.mu.Unlock()
		for i, c := range pending {
			from = c.seq
			if !c.notice {
				continue
			}
			id := fmt.Sprintf("%s-%d", x.conn, c.seq)
			name := "eventChanged"
			p := map[string]any{"id": id, "calendar": c.cal, "event": c.id}
			if c.removed {
				name = "eventRemoved"
			} else {
				p["etag"] = etags[i]
			}
			payload, _ := json.Marshal(p)
			cur, _ := json.Marshal(c.seq)
			if err := emit(adapter.Event{Contract: calendar.EventsRef, Name: name, ID: id, Payload: payload, Cursor: cur}); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func errf(code, format string, args ...any) error { return connectors.Errorf(code, format, args...) }

func (x *session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	a := x.s.account(x.tenant, x.conn)
	if ctx.Contract == ControlRef {
		return x.control(a, op, args)
	}
	if f := a.faults[op]; f != nil && f.times > 0 {
		f.times--
		if f.times == 0 {
			delete(a.faults, op)
		}
		return nil, errf(f.kind, "the test server failed %s as asked", op)
	}
	var in struct {
		Calendar  string                     `json:"calendar"`
		ID        string                     `json:"id"`
		Key       string                     `json:"key"`
		Etag      string                     `json:"etag"`
		Cursor    string                     `json:"cursor"`
		Answer    string                     `json:"answer"`
		Comment   string                     `json:"comment"`
		Event     *calendar.Event            `json:"event"`
		Set       json.RawMessage            `json:"set"`
		Window    *struct{ From, To string } `json:"window"`
		Calendars []string                   `json:"calendars"`
		People    []string                   `json:"people"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, errf(connectors.CodeInvalid, "the arguments cannot be read: %v", err)
	}
	if in.Calendar == "" {
		in.Calendar = "primary"
	}
	switch op {
	case "check":
		return json.RawMessage(`{"ok":true}`), nil
	case "calendar.list":
		return json.Marshal(map[string]any{"calendars": a.calendars})
	}
	if op != "freeBusy" && !a.hasCalendar(in.Calendar) {
		return nil, errf(connectors.CodeNotFound, "there is no calendar %q", in.Calendar)
	}
	switch op {
	case "event.list":
		return a.list(in.Calendar, in.Window, in.Cursor)
	case "event.get":
		st := a.events[in.ID]
		if st == nil || st.gone || st.cal != in.Calendar {
			return nil, errf(connectors.CodeNotFound, "there is no event %q", in.ID)
		}
		return json.Marshal(st.ev)
	case "event.create":
		if in.Event == nil || in.Key == "" {
			return nil, errf(connectors.CodeInvalid, "give the event and its key")
		}
		if id, ok := a.keys[in.Key]; ok {
			if st := a.events[id]; st != nil && !st.gone {
				return json.Marshal(st.ev)
			}
		}
		if err := valid(*in.Event); err != nil {
			return nil, err
		}
		a.next++
		ev := *in.Event
		ev.ID, ev.Etag, ev.Calendar = fmt.Sprintf("ct-%d", a.next), "1", in.Calendar
		if ev.ICalUID == "" {
			ev.ICalUID = ev.ID + "@caltest"
		}
		if ev.Status == "" {
			ev.Status = calendar.StatusConfirmed
		}
		if ev.Transparency == "" {
			ev.Transparency = calendar.Opaque
		}
		if ev.Organizer == nil && a.email != "" {
			ev.Organizer = &calendar.Person{Email: a.email, Name: a.name}
		}
		a.events[ev.ID] = &stored{ev: ev, cal: in.Calendar}
		a.keys[in.Key] = ev.ID
		a.log(in.Calendar, ev.ID, false)
		return json.Marshal(ev)
	case "event.update":
		st := a.events[in.ID]
		if st == nil || st.gone || st.cal != in.Calendar {
			return nil, errf(connectors.CodeNotFound, "there is no event %q", in.ID)
		}
		if in.Etag != "" && in.Etag != st.ev.Etag {
			return nil, errf(connectors.CodeConflict, "event %q changed since etag %s (it is at %s)", in.ID, in.Etag, st.ev.Etag)
		}
		ev, err := merge(st.ev, in.Set)
		if err != nil {
			return nil, err
		}
		if err := valid(ev); err != nil {
			return nil, err
		}
		ev.ID, ev.Calendar = st.ev.ID, st.cal
		bump(&ev)
		st.ev = ev
		a.log(st.cal, ev.ID, false)
		return json.Marshal(ev)
	case "event.delete":
		st := a.events[in.ID]
		if st == nil || st.gone || st.cal != in.Calendar {
			return json.RawMessage(`{"deleted":false}`), nil
		}
		if in.Etag != "" && in.Etag != st.ev.Etag {
			return nil, errf(connectors.CodeConflict, "event %q changed since etag %s", in.ID, in.Etag)
		}
		st.gone = true
		a.log(st.cal, st.ev.ID, true)
		return json.RawMessage(`{"deleted":true}`), nil
	case "event.respond":
		st := a.events[in.ID]
		if st == nil || st.gone || st.cal != in.Calendar {
			return nil, errf(connectors.CodeNotFound, "there is no event %q", in.ID)
		}
		switch in.Answer {
		case calendar.Accepted, calendar.Tentative, calendar.Declined:
		default:
			return nil, errf(connectors.CodeInvalid, "%q is not accepted, tentative or declined", in.Answer)
		}
		if err := answer(&st.ev, a.email, in.Answer, in.Comment); err != nil {
			return nil, err
		}
		a.log(st.cal, st.ev.ID, false)
		return json.Marshal(st.ev)
	case "freeBusy":
		if in.Window == nil {
			return nil, errf(connectors.CodeInvalid, "give a window")
		}
		w, err := parseWindow(in.Window.From, in.Window.To)
		if err != nil {
			return nil, err
		}
		out := []calendar.FreeBusy{}
		for _, c := range in.Calendars {
			if !a.hasCalendar(c) {
				out = append(out, calendar.FreeBusy{Who: c, Busy: []calendar.Busy{}, Error: "no such calendar"})
				continue
			}
			out = append(out, calendar.FreeBusy{Who: c, Busy: a.busy(c, w)})
		}
		for _, p := range in.People {
			p = strings.ToLower(strings.TrimSpace(p))
			other := x.s.byEmail(x.tenant, p)
			if other == nil {
				out = append(out, calendar.FreeBusy{Who: p, Busy: []calendar.Busy{}, Error: "no calendar for " + p})
				continue
			}
			out = append(out, calendar.FreeBusy{Who: p, Busy: other.busy("primary", w)})
		}
		return json.Marshal(map[string]any{"freeBusy": out})
	}
	return nil, errf(connectors.CodeNotSupported, "no operation %q", op)
}

func (s *Server) byEmail(tenant uint64, email string) *account {
	prefix := strconv.FormatUint(tenant, 10) + "."
	keys := make([]string, 0, len(s.conns))
	for k := range s.conns {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) && s.conns[k].email == email && email != "" {
			return s.conns[k]
		}
	}
	return nil
}

func valid(ev calendar.Event) error {
	s, err := when.ZonedInstant(ev.Start)
	if err != nil {
		return errf(connectors.CodeInvalid, "start: %v", err)
	}
	e, err := when.ZonedInstant(ev.End)
	if err != nil {
		return errf(connectors.CodeInvalid, "end: %v", err)
	}
	if e.Before(s) {
		return errf(connectors.CodeInvalid, "the event ends before it starts")
	}
	if ev.Recurrence != nil && ev.Recurrence.RRule != "" {
		if _, err := when.DescribeRule(ev.Recurrence.RRule, ""); err != nil {
			return errf(connectors.CodeInvalid, "%v", err)
		}
	}
	return nil
}

func bump(ev *calendar.Event) {
	n, _ := strconv.Atoi(ev.Etag)
	ev.Etag = strconv.Itoa(n + 1)
}

// merge applies set (fields in the event shape) to ev.
func merge(ev calendar.Event, set json.RawMessage) (calendar.Event, error) {
	base, _ := json.Marshal(ev)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(base, &m)
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(set, &patch); err != nil {
		return ev, errf(connectors.CodeInvalid, "set is not an object")
	}
	for k, v := range patch {
		if string(v) == "null" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	raw, _ := json.Marshal(m)
	var out calendar.Event
	if err := json.Unmarshal(raw, &out); err != nil {
		return ev, errf(connectors.CodeInvalid, "set does not fit the event: %v", err)
	}
	return out, nil
}

func answer(ev *calendar.Event, email, response, comment string) error {
	for i := range ev.Attendees {
		if strings.EqualFold(ev.Attendees[i].Email, email) {
			ev.Attendees[i].Response = response
			ev.Attendees[i].Comment = comment
			bump(ev)
			return nil
		}
	}
	return errf(connectors.CodeInvalid, "%s is not invited to %q", email, ev.Title)
}

func parseWindow(from, to string) (when.Window, error) {
	f, err := when.ParseInstant(from)
	if err != nil {
		return when.Window{}, errf(connectors.CodeInvalid, "window: %v", err)
	}
	t, err := when.ParseInstant(to)
	if err != nil {
		return when.Window{}, errf(connectors.CodeInvalid, "window: %v", err)
	}
	return when.Window{From: f, To: t}, nil
}

// occurrences gives an event's occurrences in a window.
func occurrences(ev calendar.Event, w when.Window) []when.Occurrence {
	s, err1 := when.ZonedInstant(ev.Start)
	e, err2 := when.ZonedInstant(ev.End)
	if err1 != nil || err2 != nil {
		return nil
	}
	r := when.Recurrence{}
	if ev.Recurrence != nil {
		r = *ev.Recurrence
	}
	os, err := when.Expand(when.ExpandArgs{Start: ev.Start, Duration: e.Sub(s), Recurrence: r, Window: w, Max: 100000})
	if err != nil {
		return nil
	}
	return os
}

func (a *account) sortedIDs() []string {
	ids := make([]string, 0, len(a.events))
	for id := range a.events {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		ni, _ := strconv.Atoi(strings.TrimPrefix(ids[i], "ct-"))
		nj, _ := strconv.Atoi(strings.TrimPrefix(ids[j], "ct-"))
		return ni < nj
	})
	return ids
}

func (a *account) list(cal string, win *struct{ From, To string }, cursor string) (json.RawMessage, error) {
	events := []calendar.Event{}
	removed := []string{}
	if cursor != "" {
		from, err := strconv.Atoi(cursor)
		if err != nil || from < 0 || from > a.cursor() {
			return nil, errf(connectors.CodeInvalid, "%s: the cursor %q is not one this server gave", calendar.CodeCursorExpired, cursor)
		}
		if from < a.expiredBefore {
			return nil, errf(connectors.CodeInvalid, "%s: the cursor is too old; list the window again", calendar.CodeCursorExpired)
		}
		seen := map[string]bool{}
		for _, c := range a.changes[from:] {
			if c.cal != cal || seen[c.id] {
				continue
			}
			seen[c.id] = true
		}
		for _, id := range a.sortedIDs() {
			if !seen[id] {
				continue
			}
			if st := a.events[id]; st.gone {
				removed = append(removed, id)
			} else {
				events = append(events, st.ev)
			}
		}
		return json.Marshal(map[string]any{"events": events, "removed": removed, "cursor": strconv.Itoa(a.cursor())})
	}
	if win == nil {
		return nil, errf(connectors.CodeInvalid, "give a window or a cursor")
	}
	w, err := parseWindow(win.From, win.To)
	if err != nil {
		return nil, err
	}
	for _, id := range a.sortedIDs() {
		st := a.events[id]
		if st.gone || st.cal != cal {
			continue
		}
		if len(occurrences(st.ev, w)) > 0 {
			events = append(events, st.ev)
		}
	}
	return json.Marshal(map[string]any{"events": events, "removed": removed, "cursor": strconv.Itoa(a.cursor())})
}

func (a *account) busy(cal string, w when.Window) []calendar.Busy {
	var evs []when.BusyEvent
	for _, id := range a.sortedIDs() {
		st := a.events[id]
		if st.gone || st.cal != cal {
			continue
		}
		resp := ""
		for _, at := range st.ev.Attendees {
			if strings.EqualFold(at.Email, a.email) {
				resp = at.Response
			}
		}
		for _, o := range occurrences(st.ev, w) {
			evs = append(evs, when.BusyEvent{Start: o.Start, End: o.End, Transparency: st.ev.Transparency, Status: st.ev.Status, Response: resp})
		}
	}
	ivs, _ := when.Busy(evs)
	out := []calendar.Busy{}
	for _, iv := range ivs {
		out = append(out, calendar.Busy{Start: iv.Start.Format(time.RFC3339), End: iv.End.Format(time.RFC3339), Type: "busy"})
	}
	return out
}

func (x *session) control(a *account, op string, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Calendar  string          `json:"calendar"`
		ID        string          `json:"id"`
		Event     *calendar.Event `json:"event"`
		Start     *when.Zoned     `json:"start"`
		End       *when.Zoned     `json:"end"`
		Title     string          `json:"title"`
		Email     string          `json:"email"`
		Response  string          `json:"response"`
		Operation string          `json:"operation"`
		Times     int             `json:"times"`
		Kind      string          `json:"kind"`
		N         int             `json:"n"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, errf(connectors.CodeInvalid, "the arguments cannot be read: %v", err)
	}
	if in.Calendar == "" {
		in.Calendar = "primary"
	}
	find := func() (*stored, error) {
		st := a.events[in.ID]
		if st == nil || st.gone || st.cal != in.Calendar {
			return nil, errf(connectors.CodeNotFound, "there is no event %q", in.ID)
		}
		return st, nil
	}
	idEtag := func(ev calendar.Event) (json.RawMessage, error) {
		return json.Marshal(map[string]string{"id": ev.ID, "etag": ev.Etag})
	}
	switch op {
	case "add":
		if in.Event == nil || !a.hasCalendar(in.Calendar) {
			return nil, errf(connectors.CodeInvalid, "give an event and an existing calendar")
		}
		if err := valid(*in.Event); err != nil {
			return nil, err
		}
		a.next++
		ev := *in.Event
		ev.ID, ev.Etag, ev.Calendar = fmt.Sprintf("ct-%d", a.next), "1", in.Calendar
		if ev.ICalUID == "" {
			ev.ICalUID = ev.ID + "@caltest"
		}
		if ev.Status == "" {
			ev.Status = calendar.StatusConfirmed
		}
		if ev.Transparency == "" {
			ev.Transparency = calendar.Opaque
		}
		a.events[ev.ID] = &stored{ev: ev, cal: in.Calendar}
		a.log(in.Calendar, ev.ID, false)
		return idEtag(ev)
	case "move":
		st, err := find()
		if err != nil {
			return nil, err
		}
		if in.Start == nil || in.End == nil {
			return nil, errf(connectors.CodeInvalid, "give start and end")
		}
		ev := st.ev
		ev.Start, ev.End = *in.Start, *in.End
		if in.Title != "" {
			ev.Title = in.Title
		}
		if err := valid(ev); err != nil {
			return nil, err
		}
		bump(&ev)
		st.ev = ev
		a.log(st.cal, ev.ID, false)
		return idEtag(ev)
	case "remove":
		st, err := find()
		if err != nil {
			return nil, err
		}
		st.gone = true
		a.log(st.cal, st.ev.ID, true)
		return json.RawMessage(`{"ok":true}`), nil
	case "answer":
		st, err := find()
		if err != nil {
			return nil, err
		}
		if err := answer(&st.ev, in.Email, in.Response, ""); err != nil {
			return nil, err
		}
		a.log(st.cal, st.ev.ID, false)
		return idEtag(st.ev)
	case "fail":
		if _, ok := calendar.EventsContract().Operation(in.Operation); !ok {
			return nil, errf(connectors.CodeInvalid, "calendar.events has no operation %q", in.Operation)
		}
		k := in.Kind
		if k == "" {
			k = connectors.CodeUnavailable
		}
		a.faults[in.Operation] = &fault{times: max(in.Times, 1), kind: k}
	case "expireCursor":
		a.expiredBefore = a.cursor()
		// The current cursor itself is too old after this.
		a.expiredBefore++
		a.log("", "", false)
		a.changes[len(a.changes)-1].notice = false
	case "dropNotifications":
		a.drop += max(in.N, 1)
	case "state":
		type calState struct {
			ID     string           `json:"id"`
			Name   string           `json:"name"`
			Events []calendar.Event `json:"events"`
		}
		cals := []calState{}
		for _, c := range a.calendars {
			cs := calState{ID: c.ID, Name: c.Name, Events: []calendar.Event{}}
			for _, id := range a.sortedIDs() {
				if st := a.events[id]; !st.gone && st.cal == c.ID {
					cs.Events = append(cs.Events, st.ev)
				}
			}
			cals = append(cals, cs)
		}
		faults := []map[string]any{}
		ops := make([]string, 0, len(a.faults))
		for o := range a.faults {
			ops = append(ops, o)
		}
		sort.Strings(ops)
		for _, o := range ops {
			faults = append(faults, map[string]any{"operation": o, "times": a.faults[o].times, "kind": a.faults[o].kind})
		}
		return json.Marshal(map[string]any{"calendars": cals, "cursor": strconv.Itoa(a.cursor()), "faults": faults, "drop": a.drop})
	default:
		return nil, errf(connectors.CodeNotSupported, "no operation %q", op)
	}
	return json.RawMessage(`{"ok":true}`), nil
}
