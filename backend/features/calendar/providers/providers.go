// Package providers holds what the outside calendar types share (forge
// 28l): reading a stranger's .ics in the child, windows, occurrences,
// computed free/busy, merging a change into an event, answering for the
// connection's own attendee, and HTTP statuses as connector errors. The
// types are calendar/providers/caldav and calendar/providers/ics.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
	"github.com/SgtFlame/foundation/backend/features/connectors"
)

// DefaultTimeout bounds one HTTP request to a calendar service.
const DefaultTimeout = 30 * time.Second

// Errorf is a connector error.
func Errorf(code, format string, args ...any) error {
	return connectors.Errorf(code, format, args...)
}

// Reader is the .ics reader a type uses: r when given (tests pass
// ical.Parse wrapped), else ical.Child over this program, which checks
// ical.ChildRequested first thing (cli/main.go). A calendar service's
// answer is a stranger's file, so a server never parses it in process.
func Reader(r ical.Reader) ical.Reader {
	if r != nil {
		return r
	}
	return func(ctx context.Context, raw []byte) (ical.Parsed, error) {
		exe, err := os.Executable()
		if err != nil {
			return ical.Parsed{}, err
		}
		return ical.Child(exe)(ctx, raw)
	}
}

// InProcess is ical.Parse as a Reader, for tests only.
func InProcess(_ context.Context, raw []byte) (ical.Parsed, error) { return ical.Parse(raw) }

// Window is event.list's and freeBusy's window.
type Window struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Parse reads the window.
func (w Window) Parse() (when.Window, error) {
	f, err := when.ParseInstant(w.From)
	if err != nil {
		return when.Window{}, Errorf(connectors.CodeInvalid, "window: %v", err)
	}
	t, err := when.ParseInstant(w.To)
	if err != nil {
		return when.Window{}, Errorf(connectors.CodeInvalid, "window: %v", err)
	}
	if !t.After(f) {
		return when.Window{}, Errorf(connectors.CodeInvalid, "the window ends before it starts")
	}
	return when.Window{From: f, To: t}, nil
}

// Occurrences gives an event's occurrences in a window.
func Occurrences(ev calendar.Event, w when.Window) []when.Occurrence {
	s, err1 := when.ZonedInstant(ev.Start)
	e, err2 := when.ZonedInstant(ev.End)
	if err1 != nil || err2 != nil {
		return nil
	}
	if e.Before(s) {
		e = s
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

// Overlaps tells whether an event (a series by its rule) has an
// occurrence in the window.
func Overlaps(ev calendar.Event, w when.Window) bool { return len(Occurrences(ev, w)) > 0 }

// Busy computes busy time in a window from events, for a service without
// free/busy: what self declined or left transparent is free.
func Busy(events []calendar.Event, self string, w when.Window) []calendar.Busy {
	var evs []when.BusyEvent
	// An exception replaces its series' occurrence.
	moved := map[string]bool{}
	for _, ev := range events {
		if ev.SeriesID != "" && ev.RecurrenceID != "" {
			moved[ev.SeriesID+"|"+ev.RecurrenceID] = true
		}
	}
	for _, ev := range events {
		resp := ""
		for _, at := range ev.Attendees {
			if self != "" && strings.EqualFold(at.Email, self) {
				resp = at.Response
			}
		}
		for _, o := range Occurrences(ev, w) {
			if ev.Recurrence != nil && moved[ev.ID+"|"+o.RecurrenceID] {
				continue
			}
			evs = append(evs, when.BusyEvent{Start: o.Start, End: o.End, Transparency: ev.Transparency, Status: ev.Status, Response: resp})
		}
	}
	ivs, _ := when.Busy(evs)
	out := []calendar.Busy{}
	for _, iv := range ivs {
		out = append(out, calendar.Busy{Start: iv.Start.Format(time.RFC3339), End: iv.End.Format(time.RFC3339), Type: "busy"})
	}
	return out
}

// Merge applies set (fields in the event shape, null clearing one) to ev.
func Merge(ev calendar.Event, set json.RawMessage) (calendar.Event, error) {
	base, _ := json.Marshal(ev)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(base, &m)
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(set, &patch); err != nil || patch == nil {
		return ev, Errorf(connectors.CodeInvalid, "set is not an object")
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
		return ev, Errorf(connectors.CodeInvalid, "set does not fit the event: %v", err)
	}
	return out, nil
}

// Valid checks an event's times and rule before it is sent.
func Valid(ev calendar.Event) error {
	if strings.TrimSpace(ev.Title) == "" {
		return Errorf(connectors.CodeInvalid, "the event needs a title")
	}
	s, err := when.ZonedInstant(ev.Start)
	if err != nil {
		return Errorf(connectors.CodeInvalid, "start: %v", err)
	}
	e, err := when.ZonedInstant(ev.End)
	if err != nil {
		return Errorf(connectors.CodeInvalid, "end: %v", err)
	}
	if e.Before(s) {
		return Errorf(connectors.CodeInvalid, "the event ends before it starts")
	}
	if ev.Recurrence != nil && ev.Recurrence.RRule != "" {
		if _, err := when.DescribeRule(ev.Recurrence.RRule, ""); err != nil {
			return Errorf(connectors.CodeInvalid, "%v", err)
		}
	}
	return nil
}

// Answer sets self's answer on ev, refusing an event self is not invited
// to.
func Answer(ev *calendar.Event, self, answer, comment string) error {
	switch answer {
	case calendar.Accepted, calendar.Tentative, calendar.Declined:
	default:
		return Errorf(connectors.CodeInvalid, "answer is accepted, tentative or declined, not %q", answer)
	}
	if self == "" {
		return Errorf(connectors.CodeInvalid, "the connection has no email of its own to answer as")
	}
	for i := range ev.Attendees {
		if strings.EqualFold(ev.Attendees[i].Email, self) {
			ev.Attendees[i].Response = answer
			ev.Attendees[i].Comment = comment
			return nil
		}
	}
	return Errorf(connectors.CodeInvalid, "%s is not invited to %q", self, ev.Title)
}

// Status turns an HTTP status the service answered into a connector
// error ("" what for the message's subject), nil for 2xx.
func Status(code int, what string, detail string) error {
	if code >= 200 && code < 300 {
		return nil
	}
	msg := fmt.Sprintf("%s: the calendar service answered %d %s", what, code, http.StatusText(code))
	if detail = strings.TrimSpace(detail); detail != "" {
		msg += ": " + detail
	}
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return Errorf(connectors.CodeAuth, "%s", msg)
	case code == http.StatusNotFound || code == http.StatusGone:
		return Errorf(connectors.CodeNotFound, "%s", msg)
	case code == http.StatusConflict || code == http.StatusPreconditionFailed:
		return Errorf(connectors.CodeConflict, "%s", msg)
	case code == http.StatusTooManyRequests:
		return Errorf(connectors.CodeRateLimited, "%s", msg)
	case code == http.StatusNotImplemented || code == http.StatusMethodNotAllowed:
		return Errorf(connectors.CodeNotSupported, "%s", msg)
	case code >= 500:
		return Errorf(connectors.CodeUnavailable, "%s", msg)
	}
	return Errorf(connectors.CodeInvalid, "%s", msg)
}

// Transport turns a failed request into a retryable connector error.
func Transport(what string, err error) error {
	return Errorf(connectors.CodeUnavailable, "%s: the calendar service could not be reached: %v", what, err)
}

// ReadOnly is the error every write of a read-only calendar gives.
func ReadOnly(op string) error {
	return Errorf(connectors.CodeNotSupported, "%s: this calendar is a read-only feed", op)
}
