package ical

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
)

// iTIP methods.
const (
	MethodPublish        = "PUBLISH"
	MethodRequest        = "REQUEST"
	MethodReply          = "REPLY"
	MethodCancel         = "CANCEL"
	MethodCounter        = "COUNTER"
	MethodDeclineCounter = "DECLINECOUNTER"
)

// Methods are the methods Compose writes.
var Methods = []string{MethodPublish, MethodRequest, MethodReply, MethodCancel, MethodCounter, MethodDeclineCounter}

// ProdID is the PRODID composed files carry.
const ProdID = "-//Axiom//Calendar 1.0//EN"

// writer writes content lines, folded at 75 octets and ended by CRLF.
type writer struct{ b strings.Builder }

func (w *writer) line(name string, params []string, value string) {
	s := name
	for _, p := range params {
		s += ";" + p
	}
	s += ":" + value
	// Fold at 75 octets, never inside a UTF-8 sequence.
	first := true
	for len(s) > 0 {
		limit := 75
		if !first {
			limit = 74
		}
		if len(s) <= limit {
			if !first {
				w.b.WriteByte(' ')
			}
			w.b.WriteString(s)
			break
		}
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if !first {
			w.b.WriteByte(' ')
		}
		w.b.WriteString(s[:cut])
		w.b.WriteString("\r\n")
		s = s[cut:]
		first = false
	}
	w.b.WriteString("\r\n")
}

// escape writes a TEXT value.
func escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(s)
}

// param quotes a parameter value when it needs it, dropping what a
// parameter cannot hold.
func param(name, v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '"' || r == '\r' || r == '\n' || r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, v)
	if strings.ContainsAny(v, ";:,") {
		v = `"` + v + `"`
	}
	return name + "=" + v
}

const utcLayout = "20060102T150405Z"

var partstats = map[string]string{calendar.NeedsAction: "NEEDS-ACTION", calendar.Accepted: "ACCEPTED", calendar.Tentative: "TENTATIVE", calendar.Declined: "DECLINED", calendar.Delegated: "DELEGATED"}

// ComposeOptions are what else a composed file carries.
type ComposeOptions struct {
	Now time.Time // DTSTAMP; now when zero
	// Resource writes a calendar object resource for a CalDAV server
	// (forge 28l): no METHOD line, which RFC 4791 section 4.1 forbids.
	Resource bool
}

// Compose writes events as .ics with a method (PUBLISH when empty), with
// the VTIMEZONE blocks its zones need, folded lines and escaped text.
func Compose(method string, events []calendar.Event, o ComposeOptions) (string, error) {
	method = strings.ToUpper(method)
	if method == "" {
		method = MethodPublish
	}
	ok := false
	for _, m := range Methods {
		ok = ok || m == method
	}
	if !ok {
		return "", fmt.Errorf("%q is not an iTIP method", method)
	}
	if len(events) == 0 {
		return "", errors.New("give at least one event")
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	var w writer
	w.line("BEGIN", nil, "VCALENDAR")
	w.line("PRODID", nil, ProdID)
	w.line("VERSION", nil, "2.0")
	w.line("CALSCALE", nil, "GREGORIAN")
	if !o.Resource {
		w.line("METHOD", nil, method)
	}
	// The zones the events use and the years they span.
	zones := map[string][2]int{}
	note := func(z when.Zoned) {
		if z.Date != "" || z.Zone == "" || z.Zone == "UTC" || z.Zone == "Etc/UTC" {
			return
		}
		t, err := when.ZonedInstant(z)
		if err != nil {
			return
		}
		y := t.Year()
		span, seen := zones[z.Zone]
		if !seen {
			span = [2]int{y, y}
		}
		span[0], span[1] = min(span[0], y), max(span[1], y)
		if span[1]-span[0] > 20 {
			span[1] = span[0] + 20
		}
		zones[z.Zone] = span
	}
	for _, e := range events {
		note(e.Start)
		note(e.End)
	}
	names := make([]string, 0, len(zones))
	for z := range zones {
		names = append(names, z)
	}
	sort.Strings(names)
	for _, z := range names {
		if err := vtimezone(&w, z, zones[z][0]-1, zones[z][1]+1); err != nil {
			return "", err
		}
	}
	for _, e := range events {
		if err := vevent(&w, method, e, o.Now); err != nil {
			return "", err
		}
	}
	w.line("END", nil, "VCALENDAR")
	return w.b.String(), nil
}

func dateTime(w *writer, name string, z when.Zoned) error {
	if z.Date != "" {
		d, err := time.Parse(when.DateLayout, z.Date)
		if err != nil {
			return fmt.Errorf("%s: %q is not a date", name, z.Date)
		}
		w.line(name, []string{"VALUE=DATE"}, d.Format("20060102"))
		return nil
	}
	t, err := when.ZonedInstant(z)
	if err != nil {
		return fmt.Errorf("%s: %v", name, err)
	}
	if z.Zone == "" || z.Zone == "UTC" || z.Zone == "Etc/UTC" {
		w.line(name, nil, t.UTC().Format(utcLayout))
		return nil
	}
	loc, _ := when.Location(z.Zone)
	w.line(name, []string{param("TZID", loc.String())}, t.In(loc).Format("20060102T150405"))
	return nil
}

func ridValue(w *writer, name string, rid string) {
	if len(rid) == 10 {
		if d, err := time.Parse(when.DateLayout, rid); err == nil {
			w.line(name, []string{"VALUE=DATE"}, d.Format("20060102"))
			return
		}
	}
	if t, err := time.Parse(time.RFC3339, rid); err == nil {
		w.line(name, nil, t.UTC().Format(utcLayout))
	}
}

func vevent(w *writer, method string, e calendar.Event, now time.Time) error {
	uid := e.ICalUID
	if uid == "" {
		uid = e.ID
	}
	if uid == "" {
		return fmt.Errorf("event %q has no UID or id", e.Title)
	}
	w.line("BEGIN", nil, "VEVENT")
	w.line("UID", nil, escape(uid))
	w.line("DTSTAMP", nil, now.UTC().Format(utcLayout))
	if e.RecurrenceID != "" {
		ridValue(w, "RECURRENCE-ID", e.RecurrenceID)
	}
	if err := dateTime(w, "DTSTART", e.Start); err != nil {
		return err
	}
	if e.End.Date != "" || e.End.Local != "" {
		if err := dateTime(w, "DTEND", e.End); err != nil {
			return err
		}
	}
	w.line("SEQUENCE", nil, fmt.Sprint(e.Sequence))
	if e.Title != "" {
		w.line("SUMMARY", nil, escape(e.Title))
	}
	if r := e.Recurrence; r != nil && method != MethodReply {
		if r.RRule != "" {
			w.line("RRULE", nil, strings.TrimPrefix(strings.TrimSpace(r.RRule), "RRULE:"))
		}
		for _, d := range r.RDate {
			ridValue(w, "RDATE", d)
		}
		for _, d := range r.ExDate {
			ridValue(w, "EXDATE", d)
		}
	}
	status := e.Status
	if method == MethodCancel {
		status = calendar.StatusCancelled
	}
	if status != "" {
		w.line("STATUS", nil, strings.ToUpper(status))
	}
	if e.Transparency == calendar.Transparent {
		w.line("TRANSP", nil, "TRANSPARENT")
	} else if method != MethodReply {
		w.line("TRANSP", nil, "OPAQUE")
	}
	if e.Location != "" {
		w.line("LOCATION", nil, escape(e.Location))
	}
	if e.Description != "" && method != MethodReply {
		w.line("DESCRIPTION", nil, escape(e.Description))
	}
	if e.URL != "" {
		w.line("URL", nil, e.URL)
	}
	if e.VideoLink != "" && method != MethodReply {
		w.line("CONFERENCE", []string{"VALUE=URI", "FEATURE=VIDEO"}, e.VideoLink)
	}
	if e.Organizer != nil && e.Organizer.Email != "" {
		var ps []string
		if e.Organizer.Name != "" {
			ps = append(ps, param("CN", e.Organizer.Name))
		}
		w.line("ORGANIZER", ps, "mailto:"+e.Organizer.Email)
	}
	for _, a := range e.Attendees {
		if a.Email == "" {
			continue
		}
		ps := []string{}
		if a.Name != "" {
			ps = append(ps, param("CN", a.Name))
		}
		ps = append(ps, "CUTYPE=INDIVIDUAL")
		ps = append(ps, "PARTSTAT="+partstatOf(a.Response))
		if a.Optional {
			ps = append(ps, "ROLE=OPT-PARTICIPANT")
		} else {
			ps = append(ps, "ROLE=REQ-PARTICIPANT")
		}
		if method == MethodRequest && !a.Organizer && (a.Response == "" || a.Response == calendar.NeedsAction) {
			ps = append(ps, "RSVP=TRUE")
		}
		w.line("ATTENDEE", ps, "mailto:"+a.Email)
		if a.Comment != "" && method == MethodReply {
			w.line("COMMENT", nil, escape(a.Comment))
		}
	}
	w.line("END", nil, "VEVENT")
	return nil
}

func partstatOf(r string) string {
	if p := partstats[r]; p != "" {
		return p
	}
	return "NEEDS-ACTION"
}

// vtimezone writes a zone's VTIMEZONE with each offset change from one
// year to another as its own STANDARD or DAYLIGHT part, read from the
// embedded zone database, so any reader agrees with us.
func vtimezone(w *writer, zone string, fromYear, toYear int) error {
	loc, err := when.Location(zone)
	if err != nil {
		return err
	}
	type change struct {
		at       time.Time
		from, to int
		abbr     string
		dst      bool
	}
	var changes []change
	start := time.Date(fromYear, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(toYear+1, 1, 1, 0, 0, 0, 0, time.UTC)
	_, prev := start.In(loc).Zone()
	initial := prev
	for t := start; t.Before(end); t = t.Add(24 * time.Hour) {
		next := t.Add(24 * time.Hour)
		_, off := next.In(loc).Zone()
		if off == prev {
			continue
		}
		// Find the second it changed.
		lo, hi := t, next
		for hi.Sub(lo) > time.Second {
			mid := lo.Add(hi.Sub(lo) / 2)
			if _, o := mid.In(loc).Zone(); o == prev {
				lo = mid
			} else {
				hi = mid
			}
		}
		abbr, _ := hi.In(loc).Zone()
		changes = append(changes, change{at: hi, from: prev, to: off, abbr: abbr, dst: hi.In(loc).IsDST()})
		prev = off
	}
	w.line("BEGIN", nil, "VTIMEZONE")
	w.line("TZID", nil, loc.String())
	if len(changes) == 0 {
		abbr, _ := start.In(loc).Zone()
		w.line("BEGIN", nil, "STANDARD")
		w.line("DTSTART", nil, "19700101T000000")
		w.line("TZOFFSETFROM", nil, offset(initial))
		w.line("TZOFFSETTO", nil, offset(initial))
		w.line("TZNAME", nil, escape(abbr))
		w.line("END", nil, "STANDARD")
	}
	for _, c := range changes {
		part := "STANDARD"
		if c.dst {
			part = "DAYLIGHT"
		}
		w.line("BEGIN", nil, part)
		// DTSTART is the local time before the change.
		w.line("DTSTART", nil, c.at.In(time.FixedZone("", c.from)).Format("20060102T150405"))
		w.line("TZOFFSETFROM", nil, offset(c.from))
		w.line("TZOFFSETTO", nil, offset(c.to))
		w.line("TZNAME", nil, escape(c.abbr))
		w.line("END", nil, part)
	}
	w.line("END", nil, "VTIMEZONE")
	return nil
}

func offset(sec int) string {
	sign := '+'
	if sec < 0 {
		sign, sec = '-', -sec
	}
	return fmt.Sprintf("%c%02d%02d", sign, sec/3600, sec%3600/60)
}
