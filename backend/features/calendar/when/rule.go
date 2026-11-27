package when

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// DefaultMaxOccurrences is the calendar section's maxOccurrences default:
// at most this many occurrences come from one expansion.
const DefaultMaxOccurrences = 1000

// ErrUnbounded is a rule with neither COUNT nor UNTIL expanded without a
// window.
var ErrUnbounded = errors.New("the rule never ends: give a window to expand it in")

// ErrTooMany is an expansion past the cap.
var ErrTooMany = errors.New("the rule makes too many occurrences")

// Recurrence is a series' rule with its extra and excluded dates
// (RFC 5545 RRULE, RDATE and EXDATE). Dates are local times in the
// series' zone (2026-03-29T09:00:00), instants (RFC 3339) or, for an
// all-day series, dates.
type Recurrence struct {
	RRule  string   `json:"rrule,omitempty"`
	RDate  []string `json:"rdate,omitempty"`
	ExDate []string `json:"exdate,omitempty"`
}

// Empty tells whether r recurs at all.
func (r Recurrence) Empty() bool { return r.RRule == "" && len(r.RDate) == 0 }

// Window is a range of instants, end exclusive.
type Window struct {
	From time.Time
	To   time.Time
}

// Zero tells whether w is no window.
func (w Window) Zero() bool { return w.From.IsZero() && w.To.IsZero() }

// Occurrence is one occurrence of a series.
type Occurrence struct {
	Start        string `json:"start"`        // instant, RFC 3339 UTC
	End          string `json:"end"`          // instant, RFC 3339 UTC
	Local        string `json:"local"`        // its start in the series' zone
	RecurrenceID string `json:"recurrenceId"` // the start it would have had: the instant (a date for all-day)
	AllDay       bool   `json:"allDay,omitempty"`
	Date         string `json:"date,omitempty"` // an all-day occurrence's date
}

// ExpandArgs are what Expand needs.
type ExpandArgs struct {
	Start      Zoned
	Duration   time.Duration
	Recurrence Recurrence
	Window     Window
	Max        int
}

// cleanRule strips an RRULE: prefix and blank space.
func cleanRule(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "RRULE:")
	return s
}

// series is a parsed series: its zone, its start in that zone and its
// rule options.
type series struct {
	loc    *time.Location
	start  time.Time // in loc (UTC for all-day)
	allDay bool
	opt    *rrule.ROption
}

func parseSeries(start Zoned, rule string) (series, error) {
	var s series
	if start.Date != "" {
		d, err := time.Parse(DateLayout, start.Date)
		if err != nil {
			return s, fmt.Errorf("%q is not a date like 2026-03-29", start.Date)
		}
		s.loc, s.start, s.allDay = time.UTC, d, true
	} else {
		loc, err := Location(start.Zone)
		if err != nil {
			return s, err
		}
		l, err := ParseLocal(start.Local)
		if err != nil {
			return s, err
		}
		s.loc, s.start = loc, Instant(l, loc)
	}
	if rule = cleanRule(rule); rule != "" {
		opt, err := rrule.StrToROptionInLocation(rule, s.loc)
		if err != nil {
			return s, fmt.Errorf("the rule %q cannot be read: %v", rule, err)
		}
		opt.Dtstart = s.start
		s.opt = opt
	}
	return s, nil
}

// parseDate reads an RDATE or EXDATE value in the series' terms.
func (s series) parseDate(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if s.allDay {
		if len(v) >= 10 {
			if d, err := time.Parse(DateLayout, v[:10]); err == nil {
				return d, nil
			}
		}
		if d, err := time.Parse("20060102", v); err == nil {
			return d, nil
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	if strings.HasSuffix(v, "Z") {
		if t, err := time.Parse("20060102T150405Z", v); err == nil {
			return t, nil
		}
	}
	if l, err := ParseLocal(v); err == nil {
		return Instant(l, s.loc), nil
	}
	if d, err := time.Parse(DateLayout, v); err == nil {
		return Instant(d, s.loc), nil
	}
	return time.Time{}, fmt.Errorf("%q is not a date or time", v)
}

// Expand gives the occurrences of a series overlapping a window, in
// order, each with its recurrence id, expanded in the series' zone so a
// 09:00 weekly meeting stays at 09:00 local across a daylight-saving
// change. A series without a rule or dates is one occurrence. Without a
// window the rule must end (COUNT or UNTIL). More than Max (1000 when 0)
// occurrences is ErrTooMany.
func Expand(a ExpandArgs) ([]Occurrence, error) {
	if a.Max <= 0 {
		a.Max = DefaultMaxOccurrences
	}
	s, err := parseSeries(a.Start, a.Recurrence.RRule)
	if err != nil {
		return nil, err
	}
	if a.Duration < 0 {
		return nil, errors.New("an occurrence cannot end before it starts")
	}
	if !a.Window.Zero() && !a.Window.To.After(a.Window.From) {
		return nil, errors.New("the window ends before it starts")
	}
	if a.Window.Zero() && s.opt != nil && s.opt.Count == 0 && s.opt.Until.IsZero() {
		return nil, ErrUnbounded
	}
	set := &rrule.Set{}
	set.DTStart(s.start)
	if s.opt != nil {
		r, err := rrule.NewRRule(*s.opt)
		if err != nil {
			return nil, fmt.Errorf("the rule cannot be used: %v", err)
		}
		set.RRule(r)
	} else {
		set.RDate(s.start)
	}
	for _, v := range a.Recurrence.RDate {
		t, err := s.parseDate(v)
		if err != nil {
			return nil, fmt.Errorf("rdate: %v", err)
		}
		set.RDate(t)
	}
	excluded := map[int64]bool{}
	for _, v := range a.Recurrence.ExDate {
		t, err := s.parseDate(v)
		if err != nil {
			return nil, fmt.Errorf("exdate: %v", err)
		}
		excluded[t.Unix()] = true
	}
	var starts []time.Time
	next := set.Iterator()
	for {
		t, ok := next()
		if !ok {
			break
		}
		if !a.Window.Zero() {
			if !t.Before(a.Window.To) {
				break
			}
			end := t.Add(a.Duration)
			if a.Duration == 0 {
				end = t.Add(time.Nanosecond)
			}
			if !end.After(a.Window.From) {
				continue
			}
		}
		if excluded[t.Unix()] {
			continue
		}
		starts = append(starts, t)
		if len(starts) > a.Max {
			return nil, fmt.Errorf("%w: more than %d", ErrTooMany, a.Max)
		}
	}
	out := make([]Occurrence, 0, len(starts))
	for _, t := range starts {
		o := Occurrence{Start: t.UTC().Format(time.RFC3339), End: t.Add(a.Duration).UTC().Format(time.RFC3339), Local: t.In(s.loc).Format(LocalLayout), RecurrenceID: t.UTC().Format(time.RFC3339)}
		if s.allDay {
			o.AllDay, o.Date, o.RecurrenceID = true, t.Format(DateLayout), t.Format(DateLayout)
		}
		out = append(out, o)
	}
	return out, nil
}

// Split is a series cut at one occurrence for "this and following": the
// rule ending before it and the rule starting from it.
type Split struct {
	Before      string `json:"before"`      // the first series' rule; empty when nothing is left before
	BeforeEmpty bool   `json:"beforeEmpty"` // the cut is the first occurrence, so the first series goes
	After       string `json:"after"`       // the second series' rule
	AfterStart  Zoned  `json:"afterStart"`  // the second series' start, the cut occurrence
}

// SplitRule cuts a series' rule at the occurrence whose recurrence id is
// rid: the first rule ends before it (UNTIL just before, or the COUNT of
// those before), the second starts from it (with the COUNT left).
func SplitRule(start Zoned, rule string, rid string) (Split, error) {
	s, err := parseSeries(start, rule)
	if err != nil {
		return Split{}, err
	}
	if s.opt == nil {
		return Split{}, errors.New("there is no rule to split")
	}
	at, err := s.parseDate(rid)
	if err != nil {
		return Split{}, fmt.Errorf("recurrence id: %v", err)
	}
	r, err := rrule.NewRRule(*s.opt)
	if err != nil {
		return Split{}, err
	}
	before := 0
	next := r.Iterator()
	for {
		t, ok := next()
		if !ok || !t.Before(at) {
			break
		}
		before++
		if before > 100000 {
			return Split{}, ErrTooMany
		}
	}
	first, second := *s.opt, *s.opt
	first.Dtstart, second.Dtstart = time.Time{}, time.Time{}
	var out Split
	if first.Count > 0 {
		first.Count = before
		second.Count = s.opt.Count - before
		if second.Count <= 0 {
			return Split{}, errors.New("the occurrence is past the series' end")
		}
	} else {
		first.Until = at.Add(-time.Second).UTC()
	}
	if before == 0 {
		out.BeforeEmpty = true
	} else {
		out.Before = first.RRuleString()
	}
	out.After = second.RRuleString()
	if s.allDay {
		out.AfterStart = Zoned{Date: at.Format(DateLayout)}
	} else {
		out.AfterStart = Zoned{Local: at.In(s.loc).Format(LocalLayout), Zone: s.loc.String()}
	}
	return out, nil
}

var weekdayNames = map[int]string{0: "Monday", 1: "Tuesday", 2: "Wednesday", 3: "Thursday", 4: "Friday", 5: "Saturday", 6: "Sunday"}

var monthNames = []string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

func ordinal(n int) string {
	if n < 0 {
		if n == -1 {
			return "last"
		}
		return ordinal(-n) + " last"
	}
	switch n {
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	case 4:
		return "fourth"
	case 5:
		return "fifth"
	}
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func list(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// DescribeRule gives a rule in plain English, such as "every second
// Tuesday until 30 June 2026", for the event editor and the model. zone
// reads a local UNTIL; "" is UTC.
func DescribeRule(rule string, zone string) (string, error) {
	loc, err := Location(zone)
	if err != nil {
		return "", err
	}
	rule = cleanRule(rule)
	if rule == "" {
		return "does not repeat", nil
	}
	o, err := rrule.StrToROptionInLocation(rule, loc)
	if err != nil {
		return "", fmt.Errorf("the rule %q cannot be read: %v", rule, err)
	}
	n := o.Interval
	if n <= 0 {
		n = 1
	}
	unit := map[rrule.Frequency]string{rrule.YEARLY: "year", rrule.MONTHLY: "month", rrule.WEEKLY: "week", rrule.DAILY: "day", rrule.HOURLY: "hour", rrule.MINUTELY: "minute", rrule.SECONDLY: "second"}[o.Freq]
	every := "every " + unit
	switch {
	case n == 2:
		every = "every other " + unit
	case n > 2:
		every = fmt.Sprintf("every %d %ss", n, unit)
	}
	var days []string
	weekdaysOnly := len(o.Byweekday) == 5 && o.Freq <= rrule.WEEKLY
	for _, d := range o.Byweekday {
		name := weekdayNames[d.Day()]
		if d.N() != 0 {
			name = "the " + ordinal(d.N()) + " " + name
		}
		days = append(days, name)
		if d.N() != 0 || d.Day() > 4 {
			weekdaysOnly = false
		}
	}
	if len(o.Bysetpos) > 0 && len(days) > 0 {
		var pos []string
		for _, p := range o.Bysetpos {
			pos = append(pos, ordinal(p))
		}
		days = []string{"the " + list(pos) + " of " + list(days)}
		weekdaysOnly = false
	}
	var parts []string
	switch {
	case o.Freq == rrule.WEEKLY && weekdaysOnly && n == 1:
		parts = append(parts, "every weekday")
	case o.Freq == rrule.WEEKLY && len(days) > 0 && n == 1:
		parts = append(parts, "every "+list(days))
	case o.Freq == rrule.WEEKLY && len(days) > 0 && n == 2:
		parts = append(parts, "every other "+list(days))
	case o.Freq == rrule.WEEKLY && len(days) > 0:
		parts = append(parts, fmt.Sprintf("every %s %s", ordinal(n), list(days)))
	case o.Freq == rrule.DAILY && weekdaysOnly:
		parts = append(parts, "every weekday")
	case len(days) > 0:
		parts = append(parts, every+" on "+list(days))
	default:
		parts = append(parts, every)
	}
	if len(o.Bymonthday) > 0 {
		var ds []string
		for _, d := range o.Bymonthday {
			ds = append(ds, "the "+ordinal(d))
		}
		parts = append(parts, "on "+list(ds))
	}
	if len(o.Bymonth) > 0 {
		var ms []string
		for _, m := range o.Bymonth {
			if m >= 1 && m <= 12 {
				ms = append(ms, monthNames[m])
			}
		}
		parts = append(parts, "in "+list(ms))
	}
	switch {
	case o.Count == 1:
		parts = append(parts, "once")
	case o.Count > 1:
		parts = append(parts, fmt.Sprintf("%d times", o.Count))
	case !o.Until.IsZero():
		parts = append(parts, "until "+o.Until.In(loc).Format("2 January 2006"))
	}
	return strings.Join(parts, " "), nil
}
