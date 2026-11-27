package when

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Interval is a busy or free range of instants, end exclusive.
type Interval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// BusyEvent is what busy time is made of: an event's or occurrence's
// times, whether it shows as busy and the person's answer.
type BusyEvent struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	Transparency string `json:"transparency,omitempty"` // opaque (busy, the default) or transparent (free)
	Status       string `json:"status,omitempty"`       // cancelled events are not busy
	Response     string `json:"response,omitempty"`     // declined events are not busy
}

// Busy merges events into busy intervals, in order, ignoring free,
// cancelled and declined ones.
func Busy(events []BusyEvent) ([]Interval, error) {
	var out []Interval
	for _, e := range events {
		if strings.EqualFold(e.Transparency, "transparent") || strings.EqualFold(e.Status, "cancelled") || strings.EqualFold(e.Response, "declined") {
			continue
		}
		s, err := ParseInstant(e.Start)
		if err != nil {
			return nil, err
		}
		en, err := ParseInstant(e.End)
		if err != nil {
			return nil, err
		}
		if !en.After(s) {
			continue
		}
		out = append(out, Interval{Start: s.UTC(), End: en.UTC()})
	}
	return Merge(out), nil
}

// Merge sorts intervals and joins those that overlap or touch.
func Merge(in []Interval) []Interval {
	if len(in) == 0 {
		return []Interval{}
	}
	s := append([]Interval(nil), in...)
	sort.Slice(s, func(i, j int) bool { return s[i].Start.Before(s[j].Start) })
	out := []Interval{s[0]}
	for _, iv := range s[1:] {
		last := &out[len(out)-1]
		if !iv.Start.After(last.End) {
			if iv.End.After(last.End) {
				last.End = iv.End
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// Span is a part of a day, "09:00" to "17:00", in local time.
type Span struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// WorkingHours is a weekly pattern of working time in a zone: per weekday
// (mon … sun) the spans worked.
type WorkingHours struct {
	Zone string            `json:"zone"`
	Days map[string][]Span `json:"days"`
}

// DefaultWorkingHours is 09:00 to 17:00, Monday to Friday, in zone.
func DefaultWorkingHours(zone string) WorkingHours {
	d := []Span{{Start: "09:00", End: "17:00"}}
	return WorkingHours{Zone: zone, Days: map[string][]Span{"mon": d, "tue": d, "wed": d, "thu": d, "fri": d}}
}

var dayKeys = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func clock(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || h < 0 || h > 24 || m < 0 || m > 59 {
		return 0, fmt.Errorf("%q is not a time of day like 09:00", s)
	}
	return h*60 + m, nil
}

// Within gives the working intervals of wh inside a window.
func (wh WorkingHours) Within(w Window) ([]Interval, error) {
	loc, err := Location(wh.Zone)
	if err != nil {
		return nil, err
	}
	var out []Interval
	day := w.From.In(loc)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	for n := 0; !day.After(w.To) && n < 800; n++ {
		for _, sp := range wh.Days[dayKeys[day.Weekday()]] {
			a, err := clock(sp.Start)
			if err != nil {
				return nil, err
			}
			b, err := clock(sp.End)
			if err != nil {
				return nil, err
			}
			s := Instant(time.Date(day.Year(), day.Month(), day.Day(), a/60, a%60, 0, 0, loc), loc)
			e := Instant(time.Date(day.Year(), day.Month(), day.Day(), b/60, b%60, 0, 0, loc), loc)
			if s.Before(w.From) {
				s = w.From
			}
			if e.After(w.To) {
				e = w.To
			}
			if e.After(s) {
				out = append(out, Interval{Start: s.UTC(), End: e.UTC()})
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return Merge(out), nil
}

// Person is one attendee's busy time and hours for FreeSlots.
type Person struct {
	ID    string       `json:"id"`
	Busy  []Interval   `json:"busy"`
	Hours WorkingHours `json:"hours"`
	// Optional people are left out of the must-fit check but count in the
	// ranking.
	Optional bool `json:"optional,omitempty"`
}

// SlotArgs are what FreeSlots needs.
type SlotArgs struct {
	People   []Person
	Duration time.Duration
	Window   Window
	Step     time.Duration // 30 minutes when 0
	Before   time.Duration // a buffer free before each slot
	After    time.Duration // and after
	Count    int           // at most this many slots; 3 when 0
	Spread   bool          // at most one slot a day while others remain
}

// SlotLocal is a slot's start in one person's zone.
type SlotLocal struct {
	Person string `json:"person"`
	Zone   string `json:"zone"`
	Local  string `json:"local"`
}

// Slot is a candidate time.
type Slot struct {
	Start string      `json:"start"`
	End   string      `json:"end"`
	Score float64     `json:"score"`
	Local []SlotLocal `json:"local"`
}

// ErrNoPeople is FreeSlots asked for nobody.
var ErrNoPeople = errors.New("give at least one person")

// FreeSlots finds candidate slots of a duration where every required
// person is free (with buffers) and inside their working hours, on a grid
// of Step from the window's start, ranked by how early they are and how
// well they sit in each person's day (the middle of the working day
// scores best). With Spread, slots are taken one per day first.
func FreeSlots(a SlotArgs) ([]Slot, error) {
	if len(a.People) == 0 {
		return nil, ErrNoPeople
	}
	if a.Duration <= 0 {
		return nil, errors.New("give a duration")
	}
	if !a.Window.To.After(a.Window.From) {
		return nil, errors.New("the window ends before it starts")
	}
	if a.Step <= 0 {
		a.Step = 30 * time.Minute
	}
	if a.Count <= 0 {
		a.Count = 3
	}
	type person struct {
		Person
		busy  []Interval
		hours []Interval
		loc   *time.Location
	}
	ps := make([]person, len(a.People))
	for i, p := range a.People {
		if p.Hours.Zone == "" && len(p.Hours.Days) == 0 {
			p.Hours = DefaultWorkingHours("UTC")
		}
		hours, err := p.Hours.Within(a.Window)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", p.ID, err)
		}
		loc, _ := Location(p.Hours.Zone)
		ps[i] = person{Person: p, busy: Merge(p.Busy), hours: hours, loc: loc}
	}
	contains := func(ivs []Interval, s, e time.Time) bool {
		for _, iv := range ivs {
			if !s.Before(iv.Start) && !e.After(iv.End) {
				return true
			}
		}
		return false
	}
	overlaps := func(ivs []Interval, s, e time.Time) bool {
		for _, iv := range ivs {
			if s.Before(iv.End) && e.After(iv.Start) {
				return true
			}
		}
		return false
	}
	type cand struct {
		s, e  time.Time
		score float64
	}
	var cands []cand
	total := a.Window.To.Sub(a.Window.From).Hours()
	start := a.Window.From.Truncate(a.Step)
	if start.Before(a.Window.From) {
		start = start.Add(a.Step)
	}
	for t, n := start, 0; !t.Add(a.Duration).After(a.Window.To) && n < 20000; t, n = t.Add(a.Step), n+1 {
		e := t.Add(a.Duration)
		ok := true
		fit := 0.0
		for _, p := range ps {
			free := !overlaps(p.busy, t.Add(-a.Before), e.Add(a.After))
			inHours := contains(p.hours, t, e)
			if !p.Optional && (!free || !inHours) {
				ok = false
				break
			}
			if free && inHours {
				fit += middleness(t, e, p.hours)
			}
		}
		if !ok {
			continue
		}
		early := 1 - t.Sub(a.Window.From).Hours()/math.Max(total, 1)
		score := math.Round((0.6*early+0.4*fit/float64(len(ps)))*1000) / 1000
		cands = append(cands, cand{s: t, e: e, score: score})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	var chosen []cand
	taken := func(c cand) bool {
		for _, x := range chosen {
			if c.s.Before(x.e) && c.e.After(x.s) {
				return true
			}
		}
		return false
	}
	day := func(t time.Time) string { return t.UTC().Format(DateLayout) }
	if a.Spread {
		days := map[string]bool{}
		for _, c := range cands {
			if len(chosen) == a.Count {
				break
			}
			if !days[day(c.s)] && !taken(c) {
				days[day(c.s)] = true
				chosen = append(chosen, c)
			}
		}
	}
	for _, c := range cands {
		if len(chosen) == a.Count {
			break
		}
		if !taken(c) {
			chosen = append(chosen, c)
		}
	}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].s.Before(chosen[j].s) })
	out := make([]Slot, 0, len(chosen))
	for _, c := range chosen {
		sl := Slot{Start: c.s.UTC().Format(time.RFC3339), End: c.e.UTC().Format(time.RFC3339), Score: c.score, Local: []SlotLocal{}}
		for _, p := range ps {
			sl.Local = append(sl.Local, SlotLocal{Person: p.ID, Zone: p.loc.String(), Local: c.s.In(p.loc).Format(LocalLayout)})
		}
		out = append(out, sl)
	}
	return out, nil
}

// middleness is how near a slot is to the middle of the working span
// holding it: 1 in the middle, 0 at an edge.
func middleness(s, e time.Time, hours []Interval) float64 {
	for _, iv := range hours {
		if !s.Before(iv.Start) && !e.After(iv.End) {
			span := iv.End.Sub(iv.Start).Minutes()
			if span <= 0 {
				return 0
			}
			mid := iv.Start.Add(iv.End.Sub(iv.Start) / 2)
			c := s.Add(e.Sub(s) / 2)
			d := math.Abs(c.Sub(mid).Minutes())
			return math.Max(0, 1-d/(span/2))
		}
	}
	return 0
}
