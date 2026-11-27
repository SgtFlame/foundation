// Package contracttest is the suite every calendar.events@1 type passes
// (forge 28c): the Calendar test server here, the CalDAV and ICS types
// against recorded responses in 28l, Google and Microsoft in 28j and 28k.
package contracttest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/connectors"
)

// Caller calls one calendar.events operation of the type under test.
type Caller func(op string, args any) (json.RawMessage, error)

// Options say what the suite may use.
type Options struct {
	// Calendar is a writable calendar's id; the primary one when empty.
	Calendar string
	// Self is the connection's own email, its attendee entry.
	Self string
	// Expire makes every cursor given so far too old (the test server's
	// expireCursor); nil skips that step.
	Expire func() error
	// ReadOnly types (a subscribed feed) are checked only to list and to
	// refuse writes with notSupported.
	ReadOnly bool
	// NoSeries skips the recurring series step, for a recording without it.
	NoSeries bool
}

type listed struct {
	Events  []calendar.Event `json:"events"`
	Removed []string         `json:"removed"`
	Cursor  string           `json:"cursor"`
}

// Run runs the suite: create is keyed; an update with a stale etag is
// refused; a window lists what create made, a recurring series as one
// event with its rule; the cursor gives only changes and an expired one
// is reported as cursor_expired; delete is idempotent; respond changes
// only the caller's attendee entry.
func Run(t *testing.T, call Caller, o Options) {
	t.Helper()
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
		return ""
	}
	if _, err := call("check", map[string]any{}); err != nil {
		t.Fatalf("check: %v", err)
	}
	var cals struct{ Calendars []calendar.Calendar }
	must("calendar.list", map[string]any{}, &cals)
	if len(cals.Calendars) == 0 {
		t.Fatal("calendar.list is empty")
	}
	cal := o.Calendar
	if cal == "" {
		for _, c := range cals.Calendars {
			if c.Primary {
				cal = c.ID
			}
		}
		if cal == "" {
			cal = cals.Calendars[0].ID
		}
	}
	window := map[string]string{"from": "2026-11-30T00:00:00Z", "to": "2026-12-07T00:00:00Z"}
	var first listed
	must("event.list", map[string]any{"calendar": cal, "window": window}, &first)
	if first.Cursor == "" {
		t.Fatal("event.list gave no cursor")
	}
	ev := map[string]any{"title": "Contract suite", "start": map[string]string{"local": "2026-12-01T10:00:00", "zone": "Europe/London"},
		"end": map[string]string{"local": "2026-12-01T11:00:00", "zone": "Europe/London"}}
	if o.ReadOnly {
		_, err := call("event.create", map[string]any{"calendar": cal, "key": "suite-1", "event": ev})
		if code(err) != connectors.CodeNotSupported {
			t.Fatalf("a read-only type's create = %v, want notSupported", err)
		}
		return
	}
	var a, b calendar.Event
	must("event.create", map[string]any{"calendar": cal, "key": "suite-1", "event": ev}, &a)
	if a.ID == "" || a.Etag == "" {
		t.Fatalf("event.create gave %+v", a)
	}
	must("event.create", map[string]any{"calendar": cal, "key": "suite-1", "event": ev}, &b)
	if b.ID != a.ID {
		t.Fatalf("a second create with the same key made another event: %s then %s", a.ID, b.ID)
	}
	var w listed
	must("event.list", map[string]any{"calendar": cal, "window": window}, &w)
	if n := count(w.Events, a.ID); n != 1 {
		t.Fatalf("the window lists %d events %s", n, a.ID)
	}
	var got calendar.Event
	must("event.get", map[string]any{"calendar": cal, "id": a.ID}, &got)
	if got.Title != "Contract suite" || got.Start.Local != "2026-12-01T10:00:00" {
		t.Fatalf("event.get gave %+v", got)
	}
	// An update based on the right etag works and moves it; one based on
	// the old etag is refused.
	var up calendar.Event
	must("event.update", map[string]any{"calendar": cal, "id": a.ID, "etag": a.Etag, "set": map[string]any{"title": "Contract suite (moved)"}}, &up)
	if up.Etag == a.Etag || up.Title != "Contract suite (moved)" {
		t.Fatalf("event.update gave %+v", up)
	}
	if _, err := call("event.update", map[string]any{"calendar": cal, "id": a.ID, "etag": a.Etag, "set": map[string]any{"title": "stale"}}); code(err) != connectors.CodeConflict {
		t.Fatalf("a stale etag's update = %v, want conflict", err)
	}
	// The cursor from before gives only the changes since.
	var delta listed
	must("event.list", map[string]any{"calendar": cal, "cursor": first.Cursor}, &delta)
	if count(delta.Events, a.ID) != 1 || delta.Cursor == "" {
		t.Fatalf("the delta = %+v", delta)
	}
	var none listed
	must("event.list", map[string]any{"calendar": cal, "cursor": delta.Cursor}, &none)
	if len(none.Events) != 0 || len(none.Removed) != 0 {
		t.Fatalf("a delta with nothing new = %+v", none)
	}
	if !o.NoSeries {
		var series calendar.Event
		must("event.create", map[string]any{"calendar": cal, "key": "suite-series", "event": map[string]any{
			"title": "Weekly", "start": map[string]string{"local": "2026-11-02T09:00:00", "zone": "America/New_York"},
			"end": map[string]string{"local": "2026-11-02T09:30:00", "zone": "America/New_York"}, "recurrence": map[string]any{"rrule": "FREQ=WEEKLY;BYDAY=MO"}}}, &series)
		var sw listed
		must("event.list", map[string]any{"calendar": cal, "window": window}, &sw)
		if count(sw.Events, series.ID) != 1 {
			t.Fatalf("the series is listed %d times", count(sw.Events, series.ID))
		}
		for _, e := range sw.Events {
			if e.ID == series.ID && (e.Recurrence == nil || !strings.Contains(e.Recurrence.RRule, "FREQ=WEEKLY")) {
				t.Fatalf("the series lost its rule: %+v", e)
			}
		}
	}
	// Respond changes only the caller's attendee entry.
	if o.Self != "" {
		var inv calendar.Event
		must("event.create", map[string]any{"calendar": cal, "key": "suite-invite", "event": map[string]any{
			"title": "Invite", "start": map[string]string{"local": "2026-12-02T10:00:00", "zone": "UTC"}, "end": map[string]string{"local": "2026-12-02T11:00:00", "zone": "UTC"},
			"organizer": map[string]string{"email": "dana@example.com"},
			"attendees": []map[string]any{{"email": o.Self, "response": "needsAction"}, {"email": "mia@example.com", "response": "needsAction"}}}}, &inv)
		var answered calendar.Event
		must("event.respond", map[string]any{"calendar": cal, "id": inv.ID, "answer": "accepted"}, &answered)
		for _, at := range answered.Attendees {
			switch strings.ToLower(at.Email) {
			case strings.ToLower(o.Self):
				if at.Response != calendar.Accepted {
					t.Fatalf("the caller's answer = %q", at.Response)
				}
			default:
				if at.Response != calendar.NeedsAction {
					t.Fatalf("respond changed %s's answer to %q", at.Email, at.Response)
				}
			}
		}
	}
	// Delete is idempotent and leaves it gone, and the delta reports it.
	var del struct{ Deleted bool }
	must("event.delete", map[string]any{"calendar": cal, "id": a.ID}, &del)
	if !del.Deleted {
		t.Fatal("event.delete deleted nothing")
	}
	must("event.delete", map[string]any{"calendar": cal, "id": a.ID}, &del)
	if del.Deleted {
		t.Fatal("a second delete deleted something")
	}
	if _, err := call("event.get", map[string]any{"calendar": cal, "id": a.ID}); code(err) != connectors.CodeNotFound {
		t.Fatalf("event.get after delete = %v, want notFound", err)
	}
	var gone listed
	must("event.list", map[string]any{"calendar": cal, "cursor": delta.Cursor}, &gone)
	found := false
	for _, id := range gone.Removed {
		found = found || id == a.ID
	}
	if !found {
		t.Fatalf("the delta after delete does not report it removed: %+v", gone)
	}
	if o.Expire != nil {
		if err := o.Expire(); err != nil {
			t.Fatal(err)
		}
		_, err := call("event.list", map[string]any{"calendar": cal, "cursor": gone.Cursor})
		if code(err) != connectors.CodeInvalid || !strings.Contains(err.Error(), calendar.CodeCursorExpired) {
			t.Fatalf("an expired cursor = %v, want invalid cursor_expired", err)
		}
	}
	var fb struct{ FreeBusy []calendar.FreeBusy }
	must("freeBusy", map[string]any{"calendars": []string{cal}, "window": window}, &fb)
	if len(fb.FreeBusy) != 1 {
		t.Fatalf("freeBusy = %+v", fb)
	}
}

func count(evs []calendar.Event, id string) int {
	n := 0
	for _, e := range evs {
		if e.ID == id {
			n++
		}
	}
	return n
}
