// Package ical is the calendar's iCalendar codec (forge 28b): .ics text
// (RFC 5545) read into calendar.Event values and composed from them, with
// the scheduling methods of iTIP (RFC 5546): a reply to an invitation and
// applying an incoming message to an event. Parsing a stranger's file
// happens only in a child process (Child); composing our own values is
// safe in process.
package ical

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	goical "github.com/emersion/go-ical"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
)

// MaxBytes is the largest .ics read.
const MaxBytes = 1 << 20

// ErrTooLarge is a file over MaxBytes.
var ErrTooLarge = errors.New("the calendar file is larger than 1 MB")

// Parsed is a parsed .ics: its method, events, free/busy and what could
// not be read.
type Parsed struct {
	Method   string              `json:"method"`
	Events   []calendar.Event    `json:"events"`
	FreeBusy []calendar.FreeBusy `json:"freeBusy"`
	Problems []string            `json:"problems"`
}

// Parse reads .ics text. Only a child process calls it on a stranger's
// file (Child); tests and our own text may call it directly.
func Parse(raw []byte) (Parsed, error) {
	out := Parsed{Events: []calendar.Event{}, FreeBusy: []calendar.FreeBusy{}, Problems: []string{}}
	if len(raw) > MaxBytes {
		return out, ErrTooLarge
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !bytes.Contains(bytes.ToUpper(raw[:min(len(raw), 4096)]), []byte("BEGIN:VCALENDAR")) {
		return out, errors.New("this is not an iCalendar file")
	}
	cal, err := goical.NewDecoder(bytes.NewReader(raw)).Decode()
	if err != nil {
		return out, fmt.Errorf("the calendar file is malformed: %v", trim(err.Error()))
	}
	p := parser{zones: map[string]*time.Location{}, out: &out}
	out.Method = strings.ToUpper(propText(cal.Props, goical.PropMethod))
	for _, c := range cal.Children {
		if c.Name == goical.CompTimezone {
			p.timezone(c)
		}
	}
	for _, c := range cal.Children {
		switch c.Name {
		case goical.CompEvent:
			if e, ok := p.event(c); ok {
				out.Events = append(out.Events, e)
			}
		case goical.CompFreeBusy:
			out.FreeBusy = append(out.FreeBusy, p.freeBusy(c))
		}
	}
	return out, nil
}

func trim(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

type parser struct {
	zones map[string]*time.Location // a file's TZID to a zone
	out   *Parsed
}

func (p *parser) problem(format string, args ...any) {
	if len(p.out.Problems) < 50 {
		p.out.Problems = append(p.out.Problems, fmt.Sprintf(format, args...))
	}
}

// zoneFor maps a TZID to a zone: an IANA id, a Windows name, a vendor
// prefixed IANA id (/mozilla.org/…/Europe/London), the VTIMEZONE's
// X-LIC-LOCATION, else a fixed offset from its STANDARD part.
func (p *parser) zoneFor(tzid string) *time.Location {
	tzid = strings.Trim(tzid, `"`)
	if l, ok := p.zones[tzid]; ok {
		return l
	}
	if l, err := when.Location(tzid); err == nil {
		p.zones[tzid] = l
		return l
	}
	parts := strings.Split(tzid, "/")
	for n := min(3, len(parts)); n >= 2; n-- {
		if l, err := when.Location(strings.Join(parts[len(parts)-n:], "/")); err == nil {
			p.zones[tzid] = l
			return l
		}
	}
	return nil
}

func (p *parser) timezone(c *goical.Component) {
	tzid := propText(c.Props, goical.PropTimezoneID)
	if tzid == "" || p.zoneFor(tzid) != nil {
		return
	}
	if lic := propText(c.Props, "X-LIC-LOCATION"); lic != "" {
		if l, err := when.Location(lic); err == nil {
			p.zones[tzid] = l
			return
		}
	}
	for _, s := range c.Children {
		if s.Name != "STANDARD" {
			continue
		}
		if off, ok := parseOffset(propText(s.Props, "TZOFFSETTO")); ok {
			p.zones[tzid] = time.FixedZone(tzid, off)
			p.problem("time zone %q is not known; its standard offset is used all year", tzid)
			return
		}
	}
}

func parseOffset(s string) (int, bool) {
	if len(s) < 5 || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[1:3])
	m, err2 := strconv.Atoi(s[3:5])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	off := h*3600 + m*60
	if s[0] == '-' {
		off = -off
	}
	return off, true
}

func propText(props goical.Props, name string) string {
	pr := props.Get(name)
	if pr == nil {
		return ""
	}
	return unescape(pr.Value)
}

// unescape reads an iCalendar TEXT value without splitting it at commas,
// which many producers leave unescaped.
func unescape(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			i++
			switch v[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(v[i])
			}
			continue
		}
		b.WriteByte(v[i])
	}
	return b.String()
}

// moment is a parsed DTSTART/DTEND-like value.
type moment struct {
	t      time.Time // the instant (all-day: the date at UTC midnight)
	zoned  when.Zoned
	allDay bool
}

func (p *parser) moment(pr *goical.Prop) (moment, error) {
	v := strings.TrimSpace(pr.Value)
	if pr.ValueType() == goical.ValueDate || len(v) == 8 {
		d, err := time.Parse("20060102", v)
		if err != nil {
			return moment{}, fmt.Errorf("%q is not a date", v)
		}
		return moment{t: d, zoned: when.Zoned{Date: d.Format(when.DateLayout)}, allDay: true}, nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		if err != nil {
			return moment{}, fmt.Errorf("%q is not a date-time", v)
		}
		return moment{t: t, zoned: when.Zoned{Local: t.Format(when.LocalLayout), Zone: "UTC"}}, nil
	}
	local, err := time.Parse("20060102T150405", v)
	if err != nil {
		return moment{}, fmt.Errorf("%q is not a date-time", v)
	}
	loc := time.UTC
	if tzid := pr.Params.Get(goical.PropTimezoneID); tzid != "" {
		if l := p.zoneFor(tzid); l != nil {
			loc = l
		} else {
			p.problem("time zone %q is not known; UTC is used", tzid)
		}
	}
	t := when.Instant(local, loc)
	zone := loc.String()
	if _, err := time.LoadLocation(zone); err != nil {
		// A fixed zone from a VTIMEZONE: carry the instant in UTC.
		return moment{t: t, zoned: when.Zoned{Local: t.UTC().Format(when.LocalLayout), Zone: "UTC"}}, nil
	}
	return moment{t: t, zoned: when.Zoned{Local: local.Format(when.LocalLayout), Zone: zone}}, nil
}

// dates reads RDATE or EXDATE props into our forms: dates for all-day,
// else instants.
func (p *parser) dates(props []goical.Prop, allDay bool) []string {
	var out []string
	for _, pr := range props {
		for _, v := range strings.Split(pr.Value, ",") {
			v = strings.TrimSpace(v)
			if v == "" || strings.Contains(v, "/") { // a PERIOD
				continue
			}
			one := pr
			one.Value = v
			m, err := p.moment(&one)
			if err != nil {
				p.problem("%s %q could not be read", pr.Name, v)
				continue
			}
			if allDay || m.allDay {
				out = append(out, m.t.Format(when.DateLayout))
			} else {
				out = append(out, m.t.UTC().Format(time.RFC3339))
			}
		}
	}
	return out
}

var responses = map[string]string{"NEEDS-ACTION": calendar.NeedsAction, "ACCEPTED": calendar.Accepted, "TENTATIVE": calendar.Tentative, "DECLINED": calendar.Declined, "DELEGATED": calendar.Delegated}

func mailto(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 7 && strings.EqualFold(v[:7], "mailto:") {
		v = v[7:]
	}
	return strings.ToLower(v)
}

var videoLinkRe = regexp.MustCompile(`https://(?:meet\.google\.com/[a-z\-]+|teams\.microsoft\.com/l/meetup-join/[^\s>"]+|[a-z0-9.\-]*zoom\.us/j/[0-9]+[^\s>"]*)`)

func (p *parser) event(c *goical.Component) (calendar.Event, bool) {
	e := calendar.Event{ICalUID: propText(c.Props, goical.PropUID), Title: propText(c.Props, goical.PropSummary)}
	if e.ICalUID == "" {
		p.problem("an event without a UID was skipped")
		return e, false
	}
	st := c.Props.Get(goical.PropDateTimeStart)
	if st == nil {
		p.problem("event %q has no start and was skipped", e.ICalUID)
		return e, false
	}
	start, err := p.moment(st)
	if err != nil {
		p.problem("event %q: start %v", e.ICalUID, err)
		return e, false
	}
	e.Start = start.zoned
	var end moment
	switch {
	case c.Props.Get(goical.PropDateTimeEnd) != nil:
		end, err = p.moment(c.Props.Get(goical.PropDateTimeEnd))
		if err != nil {
			p.problem("event %q: end %v", e.ICalUID, err)
			end = start
		}
	case c.Props.Get(goical.PropDuration) != nil:
		d, derr := c.Props.Get(goical.PropDuration).Duration()
		if derr != nil {
			p.problem("event %q: its duration could not be read", e.ICalUID)
		}
		end = shift(start, d)
	case start.allDay:
		end = shift(start, 24*time.Hour)
	default:
		end = start
	}
	if end.t.Before(start.t) {
		end = start
	}
	e.End = end.zoned
	if start.zoned.Zone != "" && end.zoned.Zone != start.zoned.Zone && !end.allDay {
		// Keep both ends in the start's zone when the end has none of its own.
		if c.Props.Get(goical.PropDateTimeEnd) == nil || c.Props.Get(goical.PropDateTimeEnd).Params.Get(goical.PropTimezoneID) == "" {
			loc, _ := when.Location(start.zoned.Zone)
			e.End = when.Zoned{Local: end.t.In(loc).Format(when.LocalLayout), Zone: start.zoned.Zone}
		}
	}
	if rr := c.Props.Get(goical.PropRecurrenceRule); rr != nil || len(c.Props.Values(goical.PropRecurrenceDates)) > 0 {
		r := &when.Recurrence{RDate: p.dates(c.Props.Values(goical.PropRecurrenceDates), start.allDay), ExDate: p.dates(c.Props.Values(goical.PropExceptionDates), start.allDay)}
		if rr != nil {
			r.RRule = strings.TrimSpace(rr.Value)
		}
		e.Recurrence = r
	}
	if rid := c.Props.Get(goical.PropRecurrenceID); rid != nil {
		m, err := p.moment(rid)
		if err != nil {
			p.problem("event %q: recurrence id %v", e.ICalUID, err)
		} else if m.allDay {
			e.RecurrenceID = m.t.Format(when.DateLayout)
		} else {
			e.RecurrenceID = m.t.UTC().Format(time.RFC3339)
		}
		e.SeriesID = e.ICalUID
	}
	e.ID = e.ICalUID
	if e.RecurrenceID != "" {
		e.ID = e.ICalUID + "_" + compactID(e.RecurrenceID)
	}
	e.Status = strings.ToLower(propText(c.Props, goical.PropStatus))
	if strings.EqualFold(propText(c.Props, goical.PropTransparency), "TRANSPARENT") {
		e.Transparency = calendar.Transparent
	} else {
		e.Transparency = calendar.Opaque
	}
	e.Location = propText(c.Props, goical.PropLocation)
	e.Description = propText(c.Props, goical.PropDescription)
	if alt := c.Props.Get("X-ALT-DESC"); alt != nil && strings.EqualFold(alt.Params.Get("FMTTYPE"), "text/html") {
		e.DescriptionHTML = alt.Value
	}
	e.URL = strings.TrimSpace(propText(c.Props, goical.PropURL))
	for _, name := range []string{"X-GOOGLE-CONFERENCE", "X-MICROSOFT-SKYPETEAMSMEETINGURL", "CONFERENCE"} {
		if v := strings.TrimSpace(propText(c.Props, name)); v != "" {
			e.VideoLink = v
			break
		}
	}
	if e.VideoLink == "" {
		e.VideoLink = videoLinkRe.FindString(e.Description + " " + e.Location)
	}
	if s := propText(c.Props, goical.PropSequence); s != "" {
		e.Sequence, _ = strconv.Atoi(strings.TrimSpace(s))
	}
	if lm := c.Props.Get(goical.PropLastModified); lm != nil {
		if m, err := p.moment(lm); err == nil {
			e.Updated = m.t.UTC().Format(time.RFC3339)
		}
	}
	if o := c.Props.Get(goical.PropOrganizer); o != nil {
		e.Organizer = &calendar.Person{Email: mailto(o.Value), Name: o.Params.Get(goical.ParamCommonName)}
	}
	for _, a := range c.Props.Values(goical.PropAttendee) {
		r := responses[strings.ToUpper(a.Params.Get(goical.ParamParticipationStatus))]
		if r == "" {
			r = calendar.NeedsAction
		}
		at := calendar.Attendee{Email: mailto(a.Value), Name: a.Params.Get(goical.ParamCommonName), Response: r,
			Optional: strings.EqualFold(a.Params.Get(goical.ParamRole), "OPT-PARTICIPANT")}
		if e.Organizer != nil && at.Email == e.Organizer.Email {
			at.Organizer = true
		}
		e.Attendees = append(e.Attendees, at)
	}
	return e, true
}

// compactID is a recurrence id fit for an id: digits and T only.
func compactID(rid string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == 'T' {
			return r
		}
		return -1
	}, rid)
}

func shift(m moment, d time.Duration) moment {
	t := m.t.Add(d)
	if m.allDay {
		return moment{t: t, zoned: when.Zoned{Date: t.Format(when.DateLayout)}, allDay: true}
	}
	loc, err := when.Location(m.zoned.Zone)
	if err != nil {
		loc = time.UTC
	}
	return moment{t: t, zoned: when.Zoned{Local: t.In(loc).Format(when.LocalLayout), Zone: m.zoned.Zone}}
}

func (p *parser) freeBusy(c *goical.Component) calendar.FreeBusy {
	fb := calendar.FreeBusy{Busy: []calendar.Busy{}}
	if o := c.Props.Get(goical.PropOrganizer); o != nil {
		fb.Who = mailto(o.Value)
	}
	if a := c.Props.Get(goical.PropAttendee); a != nil {
		fb.Who = mailto(a.Value)
	}
	if st := c.Props.Get(goical.PropDateTimeStart); st != nil {
		if m, err := p.moment(st); err == nil {
			fb.Start = m.t.UTC().Format(time.RFC3339)
		}
	}
	if en := c.Props.Get(goical.PropDateTimeEnd); en != nil {
		if m, err := p.moment(en); err == nil {
			fb.End = m.t.UTC().Format(time.RFC3339)
		}
	}
	for _, pr := range c.Props.Values(goical.PropFreeBusy) {
		typ := strings.ToLower(pr.Params.Get(goical.ParamFreeBusyType))
		switch typ {
		case "", "busy":
			typ = "busy"
		case "busy-tentative":
			typ = "tentative"
		case "busy-unavailable":
			typ = "unavailable"
		case "free":
			continue
		}
		for _, period := range strings.Split(pr.Value, ",") {
			parts := strings.SplitN(strings.TrimSpace(period), "/", 2)
			if len(parts) != 2 {
				continue
			}
			s, err := time.Parse("20060102T150405Z", parts[0])
			if err != nil {
				p.problem("a free/busy period %q could not be read", period)
				continue
			}
			var e time.Time
			if strings.HasPrefix(parts[1], "P") || strings.HasPrefix(parts[1], "+P") {
				dp := goical.Prop{Value: strings.TrimPrefix(parts[1], "+")}
				d, derr := dp.Duration()
				if derr != nil {
					continue
				}
				e = s.Add(d)
			} else if e, err = time.Parse("20060102T150405Z", parts[1]); err != nil {
				continue
			}
			fb.Busy = append(fb.Busy, calendar.Busy{Start: s.Format(time.RFC3339), End: e.Format(time.RFC3339), Type: typ})
		}
	}
	sort.Slice(fb.Busy, func(i, j int) bool { return fb.Busy[i].Start < fb.Busy[j].Start })
	return fb
}
