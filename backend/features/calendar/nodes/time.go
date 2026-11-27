// Package calendarnodes contributes the calendar's nodes: the pure time
// nodes of forge 28a (Convert time zone, List time zones, Expand
// recurrence, Describe recurrence, Split recurrence, Busy intervals, Free
// slots) over calendar/when, and the iCalendar nodes of forge 28b
// (ical.go).
package calendarnodes

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features/calendar/when"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

const source = "calendar"

// Node type ids (forge 28a).
const (
	ConvertID   = "time.convert"
	ZonesID     = "time.zones"
	ExpandID    = "time.expand"
	DescribeID  = "time.describeRule"
	SplitID     = "time.splitRule"
	BusyID      = "time.busy"
	FreeSlotsID = "time.freeSlots"
)

// decode turns a pin's plain value into v.
func decode(in any, v any) error {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// plain turns a Go value into the plain values graphs pass.
func plain(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(raw, &out)
	return out, err
}

func plainList(v any) ([]any, error) {
	p, err := plain(v)
	if err != nil {
		return nil, err
	}
	l, _ := p.([]any)
	if l == nil {
		l = []any{}
	}
	return l, nil
}

func str(in cognition.Values, pin string) string {
	s, _ := cognition.Get[string](in, pin)
	return s
}

func num(in cognition.Values, pin string) float64 {
	n, _ := cognition.Get[float64](in, pin)
	return n
}

// window reads a {from, to} window pin.
func window(in cognition.Values, pin string) (when.Window, error) {
	var w struct{ From, To string }
	if err := decode(in[pin], &w); err != nil {
		return when.Window{}, fmt.Errorf("window: %v", err)
	}
	if w.From == "" && w.To == "" {
		return when.Window{}, nil
	}
	from, err := when.ParseInstant(w.From)
	if err != nil {
		return when.Window{}, fmt.Errorf("window from: %v", err)
	}
	to, err := when.ParseInstant(w.To)
	if err != nil {
		return when.Window{}, fmt.Errorf("window to: %v", err)
	}
	return when.Window{From: from, To: to}, nil
}

// maxOccurrences is the calendar section's cap, else the default.
func maxOccurrences(x *cognition.Exec) int {
	if x != nil {
		if v, err := x.Setting("calendar", "maxOccurrences"); err == nil {
			if n, ok := v.(float64); ok && n > 0 {
				return int(n)
			}
		}
	}
	return when.DefaultMaxOccurrences
}

var zonedDesc = "a zoned time {local: \"2026-03-29T09:00:00\", zone: \"Europe/London\"}, or an all-day date {date: \"2026-03-29\"}"

// Contribute registers the time nodes (forge 28a).
func Contribute(reg *extensions.Registry) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ConvertID, Source: source, Order: 480}, Title: "Convert time zone", Category: "Time",
		Description: "Converts between an instant and a local time in an IANA time zone (Windows zone names as Outlook writes them are accepted too). Given an instant (RFC 3339) and a zone, gives the local time there with its offset; given a local time (2026-03-29T09:00:00, no offset) and a zone, gives the instant. A local time that does not exist (the spring gap) moves forward by the gap, and one that happens twice (the autumn overlap) takes the earlier, as RFC 5545 says. The zone database is built into the server, so every process agrees.",
		Example:     "Convert time zone of a meeting's start instant to the attendee's zone, to write 09:00 New York time in a reply",
		Tags:        []string{"time zone", "convert", "local time", "dst", "iana", "calendar"},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "instant", Name: "Instant", Type: "string", Optional: true, Description: "An instant, RFC 3339, e.g. 2026-03-29T08:00:00Z; read when given"},
				{ID: "local", Name: "Local", Type: "string", Optional: true, Description: "A local time without an offset, read when no instant is given"},
				{ID: "zone", Name: "Zone", Type: "string", Optional: true, Description: "The IANA zone, e.g. Europe/London; UTC when empty"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("instant", "Instant", "string", "The instant, RFC 3339 in UTC"),
				cognition.DataPin("local", "Local", "string", "The local time in the zone, 2026-03-29T09:00:00"),
				cognition.DataPin("offsetMinutes", "Offset", "number", "The zone's offset from UTC then, in minutes"),
				cognition.DataPin("abbreviation", "Abbreviation", "string", "The zone's abbreviation then, e.g. BST"),
				cognition.DataPin("zone", "Zone", "string", "The zone's IANA id"),
			},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			loc, err := when.Location(str(in, "zone"))
			if err != nil {
				return nil, "", err
			}
			var t time.Time
			switch {
			case str(in, "instant") != "":
				t, err = when.ParseInstant(str(in, "instant"))
			case str(in, "local") != "":
				var l time.Time
				if l, err = when.ParseLocal(str(in, "local")); err == nil {
					t = when.Instant(l, loc)
				}
			default:
				err = errors.New("give an instant or a local time")
			}
			if err != nil {
				return nil, "", err
			}
			l := when.LocalOf(t, loc)
			return cognition.Values{"instant": l.Instant, "local": l.Local, "offsetMinutes": l.OffsetMinutes, "abbreviation": l.Abbreviation, "zone": l.Zone}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ZonesID, Source: source, Order: 481}, Title: "List time zones", Category: "Time",
		Description: "The common IANA time zones with their offsets and abbreviations at a moment (now when not given), sorted by offset, each with a label like \"(UTC+01:00) Europe/Paris\", for a zone picker; Search keeps those whose id contains it.",
		Example:     "List time zones searched by what the person typed, for the event editor's zone picker",
		Tags:        []string{"time zone", "list", "picker", "iana", "calendar"},
		Volatile:    true,
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "at", Name: "At", Type: "string", Optional: true, Description: "The moment the offsets are for, RFC 3339; now when empty"},
				{ID: "search", Name: "Search", Type: "string", Optional: true, Description: "Keep zones whose id contains this, case ignored"},
			},
			Out: []cognition.Pin{cognition.DataPin("zones", "Zones", "json", "[{id, offsetMinutes, abbreviation, label}]")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			at := time.Now()
			if s := str(in, "at"); s != "" {
				t, err := when.ParseInstant(s)
				if err != nil {
					return nil, "", err
				}
				at = t
			}
			zs, err := plainList(when.Zones(at, str(in, "search")))
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"zones": zs}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ExpandID, Source: source, Order: 482}, Title: "Expand recurrence", Category: "Time",
		Description: "The occurrences of a recurring event inside a window, in order, each {start, end, local, recurrenceId, allDay, date}: the rule (RFC 5545 RRULE with FREQ, INTERVAL, COUNT, UNTIL, BYDAY, BYMONTHDAY, BYMONTH, BYSETPOS, WKST), the series' start as " + zonedDesc + ", RDATE and EXDATE lists, and the duration. It expands in the series' own zone, so a 09:00 weekly meeting stays at 09:00 local across a daylight-saving change. An occurrence overlapping the window is included. Without a window the rule must end (COUNT or UNTIL); more occurrences than the calendar section's maxOccurrences (1000) fail the run. An event with no rule gives its one occurrence. The recurrence id is the start the occurrence would have had (the instant in UTC, or the date for all-day), the key of an exception.",
		Example:     "Expand recurrence of an event's rule over the calendar's mirror window, then write an occurrence record for each",
		Tags:        []string{"recurrence", "rrule", "expand", "occurrences", "repeat", "calendar"},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "start", Name: "Start", Type: "json", Description: "The series' first start: " + zonedDesc},
				{ID: "rrule", Name: "Rule", Type: "string", Optional: true, Description: "The RRULE, e.g. FREQ=WEEKLY;BYDAY=TU;COUNT=10; none for a one-off"},
				{ID: "rdate", Name: "Extra dates", Type: "json", Optional: true, Description: "RDATE: extra starts, local times in the series' zone, instants or dates"},
				{ID: "exdate", Name: "Excluded dates", Type: "json", Optional: true, Description: "EXDATE: starts left out, local times in the series' zone, instants or dates"},
				{ID: "durationMinutes", Name: "Duration", Type: "number", Optional: true, Description: "Each occurrence's length in minutes; 0 for a moment, 1440 a day for all-day"},
				{ID: "window", Name: "Window", Type: "json", Optional: true, Description: "{from, to} instants (RFC 3339), to exclusive; none expands a rule that ends"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("occurrences", "Occurrences", "json", "[{start, end, local, recurrenceId, allDay, date}] in order"),
				cognition.DataPin("count", "Count", "number", "How many"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var a when.ExpandArgs
			if err := decode(in["start"], &a.Start); err != nil {
				return nil, "", fmt.Errorf("start: %v", err)
			}
			a.Recurrence.RRule = str(in, "rrule")
			if err := decode(in["rdate"], &a.Recurrence.RDate); err != nil {
				return nil, "", fmt.Errorf("rdate: %v", err)
			}
			if err := decode(in["exdate"], &a.Recurrence.ExDate); err != nil {
				return nil, "", fmt.Errorf("exdate: %v", err)
			}
			a.Duration = time.Duration(num(in, "durationMinutes") * float64(time.Minute))
			w, err := window(in, "window")
			if err != nil {
				return nil, "", err
			}
			a.Window, a.Max = w, maxOccurrences(x)
			os, err := when.Expand(a)
			if err != nil {
				return nil, "", err
			}
			out, err := plainList(os)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"occurrences": out, "count": len(out)}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: DescribeID, Source: source, Order: 483}, Title: "Describe recurrence", Category: "Time",
		Description: "A recurrence rule (RRULE) in plain English, such as \"every other Tuesday until 30 June 2026\" or \"every weekday\"; an empty rule is \"does not repeat\". For the event editor's summary and for a model.",
		Example:     "Describe recurrence of the event's rule for the editor's summary line",
		Tags:        []string{"recurrence", "rrule", "describe", "english", "calendar"},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "rrule", Name: "Rule", Type: "string", Optional: true, Description: "The RRULE"},
				{ID: "zone", Name: "Zone", Type: "string", Optional: true, Description: "The zone an UNTIL date is shown in; UTC when empty"},
			},
			Out: []cognition.Pin{cognition.DataPin("text", "Text", "string", "The rule in plain English")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			s, err := when.DescribeRule(str(in, "rrule"), str(in, "zone"))
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"text": s}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: SplitID, Source: source, Order: 484}, Title: "Split recurrence", Category: "Time",
		Description: "Cuts a recurring series at one occurrence, for editing \"this and following\": the first series' rule ends just before it (UNTIL one second before, or COUNT of those before) and the second series' rule starts from it (with the COUNT left), its start the cut occurrence in the series' zone. Before empty means the cut is the series' first occurrence, so the whole series changes.",
		Example:     "Split recurrence of a weekly meeting at the occurrence a person moved, then update the master with Before and create a new master with After",
		Tags:        []string{"recurrence", "rrule", "split", "this and following", "calendar"},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "start", Name: "Start", Type: "json", Description: "The series' first start: " + zonedDesc},
				{ID: "rrule", Name: "Rule", Type: "string", Description: "The series' RRULE"},
				{ID: "recurrenceId", Name: "Recurrence id", Type: "string", Description: "The occurrence to cut at: its recurrence id (an instant, or a date for all-day)"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("before", "Before", "string", "The first series' rule; empty when nothing is left before"),
				cognition.DataPin("beforeEmpty", "Before empty", "bool", "Whether the cut is the first occurrence"),
				cognition.DataPin("after", "After", "string", "The second series' rule"),
				cognition.DataPin("afterStart", "After start", "json", "The second series' start as a zoned time or date"),
			},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var start when.Zoned
			if err := decode(in["start"], &start); err != nil {
				return nil, "", fmt.Errorf("start: %v", err)
			}
			s, err := when.SplitRule(start, str(in, "rrule"), str(in, "recurrenceId"))
			if err != nil {
				return nil, "", err
			}
			as, err := plain(s.AfterStart)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"before": s.Before, "beforeEmpty": s.BeforeEmpty, "after": s.After, "afterStart": as}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: BusyID, Source: source, Order: 485}, Title: "Busy intervals", Category: "Time",
		Description: "Merges events into busy time: a list of [{start, end, transparency, status, response}] (instants) gives the sorted busy intervals [{start, end}], with overlapping and touching ones joined and free (transparent), cancelled and declined events ignored.",
		Example:     "Busy intervals of a person's occurrences this week, for Free slots",
		Tags:        []string{"busy", "free busy", "intervals", "merge", "availability", "calendar"},
		Pins: cognition.Pins{
			In:  []cognition.Pin{{ID: "events", Name: "Events", Type: "json", Optional: true, Description: "[{start, end, transparency?, status?, response?}]"}},
			Out: []cognition.Pin{cognition.DataPin("busy", "Busy", "json", "[{start, end}] in order, merged")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var evs []when.BusyEvent
			if err := decode(in["events"], &evs); err != nil {
				return nil, "", fmt.Errorf("events: %v", err)
			}
			b, err := when.Busy(evs)
			if err != nil {
				return nil, "", err
			}
			out, err := plainList(b)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"busy": out}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: FreeSlotsID, Source: source, Order: 486}, Title: "Free slots", Category: "Time",
		Description: "Candidate meeting times for several people: each person {id, busy: [{start, end}], hours: {zone, days: {mon: [{start: \"09:00\", end: \"17:00\"}], …}}, optional}, a duration, a window and a step (30 minutes) give slots where every required person is free (with the buffers before and after) and inside their working hours, ranked by how early they are and how near the middle of everyone's working day, each {start, end, score, local: [{person, zone, local}]} with its time in every person's zone. Spread takes one slot per day first. People without hours work 09:00 to 17:00 UTC on weekdays.",
		Example:     "Free slots for the account owner and the customer next week, an hour long, three of them spread over different days",
		Tags:        []string{"free time", "slots", "scheduling", "availability", "meeting", "calendar"},
		Config:      json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","title":"Slots","min":1,"max":50,"description":"How many slots; 3 by default. The Count pin wins"},"spread":{"type":"boolean","title":"One a day first","description":"Spread slots over different days"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "people", Name: "People", Type: "json", Description: "[{id, busy, hours, optional?}]"},
				{ID: "durationMinutes", Name: "Duration", Type: "number", Description: "The meeting's length in minutes"},
				{ID: "window", Name: "Window", Type: "json", Description: "{from, to} instants to look in"},
				{ID: "stepMinutes", Name: "Step", Type: "number", Optional: true, Description: "The grid slots start on, minutes; 30 when 0"},
				{ID: "bufferBefore", Name: "Buffer before", Type: "number", Optional: true, Description: "Minutes that must be free before a slot"},
				{ID: "bufferAfter", Name: "Buffer after", Type: "number", Optional: true, Description: "Minutes that must be free after a slot"},
				{ID: "count", Name: "Count", Type: "number", Optional: true, Description: "How many slots, in place of the config's"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("slots", "Slots", "json", "[{start, end, score, local: [{person, zone, local}]}] in time order"),
				cognition.DataPin("count", "Count", "number", "How many were found"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct {
				Count  int
				Spread bool
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			var people []when.Person
			if err := decode(in["people"], &people); err != nil {
				return nil, "", fmt.Errorf("people: %v", err)
			}
			w, err := window(in, "window")
			if err != nil {
				return nil, "", err
			}
			if n := num(in, "count"); n > 0 {
				c.Count = int(n)
			}
			min := func(pin string) time.Duration { return time.Duration(num(in, pin) * float64(time.Minute)) }
			slots, err := when.FreeSlots(when.SlotArgs{People: people, Duration: min("durationMinutes"), Window: w, Step: min("stepMinutes"),
				Before: min("bufferBefore"), After: min("bufferAfter"), Count: c.Count, Spread: c.Spread})
			if err != nil {
				return nil, "", err
			}
			out, err := plainList(slots)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"slots": out, "count": len(out)}, "", nil
		},
	})
}
