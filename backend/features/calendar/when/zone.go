// Package when is the calendar's date arithmetic (forge 28a): converting
// between IANA time zones, expanding a recurrence rule within a window,
// splitting a rule for "this and following", describing a rule in plain
// English, and the interval maths free/busy is made of. Nothing here knows
// about calendars, connectors or records; the nodes in calendar/nodes wrap
// it, so it, not the library underneath, is the interface.
//
// The IANA database is embedded (time/tzdata), so every process on every
// host agrees whatever its host has installed.
package when

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // every process agrees on the zones (question 28.8)
)

// LocalLayout is a local (wall-clock) time without a zone.
const LocalLayout = "2006-01-02T15:04:05"

// DateLayout is an all-day date.
const DateLayout = "2006-01-02"

// Zoned is a local time in a zone, beside its instant: {local, zone}. An
// all-day date is {date} with no zone.
type Zoned struct {
	Local string `json:"local,omitempty"`
	Zone  string `json:"zone,omitempty"`
	Date  string `json:"date,omitempty"`
}

// AllDay tells whether z is an all-day date.
func (z Zoned) AllDay() bool { return z.Date != "" }

// ErrZone is an unknown time zone.
var ErrZone = errors.New("unknown time zone")

// Location loads an IANA zone; "" and "UTC" are UTC. Windows zone names
// (as Outlook writes them) are mapped to IANA ones.
func Location(zone string) (*time.Location, error) {
	switch zone {
	case "", "UTC", "Z", "Etc/UTC":
		return time.UTC, nil
	}
	if iana, ok := windowsZones[zone]; ok {
		zone = iana
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("%w %q", ErrZone, zone)
	}
	return loc, nil
}

// IANA maps a zone name (IANA or Windows) to its IANA id, and whether it
// is known.
func IANA(zone string) (string, bool) {
	if iana, ok := windowsZones[zone]; ok {
		return iana, true
	}
	if _, err := Location(zone); err != nil {
		return "", false
	}
	if zone == "" {
		return "UTC", true
	}
	return zone, true
}

// ParseLocal reads a local time, with or without seconds.
func ParseLocal(s string) (time.Time, error) {
	for _, l := range []string{LocalLayout, "2006-01-02T15:04", "20060102T150405"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a local time like 2026-03-29T09:00:00", s)
}

// Instant is the instant a local time in a zone names. A local time that
// does not exist (the spring gap) moves forward by the gap, and one that
// happens twice (the autumn overlap) takes the earlier, as RFC 5545 says.
func Instant(local time.Time, loc *time.Location) time.Time {
	// The wall clock read as if it were UTC.
	wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), time.UTC)
	before := offsetAt(wall.Add(-36*time.Hour), loc)
	after := offsetAt(wall.Add(36*time.Hour), loc)
	var valid []time.Time
	for _, off := range []int{before, after} {
		t := wall.Add(-time.Duration(off) * time.Second)
		if offsetAt(t, loc) == off {
			valid = append(valid, t)
		}
	}
	if len(valid) == 0 {
		// In the gap: read with the offset before it, which moves forward.
		return wall.Add(-time.Duration(before) * time.Second).In(loc)
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Before(valid[j]) })
	return valid[0].In(loc)
}

func offsetAt(t time.Time, loc *time.Location) int {
	_, off := t.In(loc).Zone()
	return off
}

// ZonedInstant is the instant of z: its local time in its zone, or the
// start of its date in UTC for an all-day date.
func ZonedInstant(z Zoned) (time.Time, error) {
	if z.Date != "" {
		d, err := time.Parse(DateLayout, z.Date)
		if err != nil {
			return time.Time{}, fmt.Errorf("%q is not a date like 2026-03-29", z.Date)
		}
		return d, nil
	}
	loc, err := Location(z.Zone)
	if err != nil {
		return time.Time{}, err
	}
	l, err := ParseLocal(z.Local)
	if err != nil {
		return time.Time{}, err
	}
	return Instant(l, loc), nil
}

// Local is an instant as a local time in a zone, with its offset in
// minutes and the zone's abbreviation then.
type Local struct {
	Local         string `json:"local"`
	Zone          string `json:"zone"`
	OffsetMinutes int    `json:"offsetMinutes"`
	Abbreviation  string `json:"abbreviation"`
	Instant       string `json:"instant"`
}

// LocalOf is the instant t in zone loc.
func LocalOf(t time.Time, loc *time.Location) Local {
	l := t.In(loc)
	abbr, off := l.Zone()
	return Local{Local: l.Format(LocalLayout), Zone: loc.String(), OffsetMinutes: off / 60, Abbreviation: abbr, Instant: t.UTC().Format(time.RFC3339)}
}

// ParseInstant reads an instant: RFC 3339, or a local time taken as UTC.
func ParseInstant(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := ParseLocal(strings.TrimSuffix(s, "Z")); err == nil {
		return t, nil
	}
	if t, err := time.Parse(DateLayout, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a time like 2026-03-29T09:00:00Z", s)
}

// ZoneInfo is a zone for a picker.
type ZoneInfo struct {
	ID            string `json:"id"`
	OffsetMinutes int    `json:"offsetMinutes"`
	Abbreviation  string `json:"abbreviation"`
	Label         string `json:"label"`
}

// Zones lists the common IANA zones with their offsets at t, by offset
// then id; search keeps those whose id contains it (case ignored).
func Zones(t time.Time, search string) []ZoneInfo {
	search = strings.ToLower(search)
	out := []ZoneInfo{}
	for _, id := range commonZones {
		if search != "" && !strings.Contains(strings.ToLower(id), search) {
			continue
		}
		loc, err := time.LoadLocation(id)
		if err != nil {
			continue
		}
		abbr, off := t.In(loc).Zone()
		sign := '+'
		if off < 0 {
			sign = '-'
		}
		a := off
		if a < 0 {
			a = -a
		}
		label := fmt.Sprintf("(UTC%c%02d:%02d) %s", sign, a/3600, a%3600/60, strings.ReplaceAll(id, "_", " "))
		out = append(out, ZoneInfo{ID: id, OffsetMinutes: off / 60, Abbreviation: abbr, Label: label})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OffsetMinutes != out[j].OffsetMinutes {
			return out[i].OffsetMinutes < out[j].OffsetMinutes
		}
		return out[i].ID < out[j].ID
	})
	return out
}
