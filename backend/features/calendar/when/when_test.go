package when

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func mustLoc(t *testing.T, z string) *time.Location {
	t.Helper()
	l, err := Location(z)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestGapAndOverlap(t *testing.T) {
	lon := mustLoc(t, "Europe/London")
	// 2026-03-29 01:30 does not exist in London: it moves forward to 02:30 BST.
	got := Instant(time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC), lon)
	if got.UTC().Format(time.RFC3339) != "2026-03-29T01:30:00Z" || got.Format("15:04") != "02:30" {
		t.Fatalf("gap: %s (%s local)", got.UTC(), got.Format("15:04 MST"))
	}
	// 2026-10-25 01:30 happens twice: the earlier, BST, is taken.
	got = Instant(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), lon)
	if got.UTC().Format(time.RFC3339) != "2026-10-25T00:30:00Z" {
		t.Fatalf("overlap: %s", got.UTC())
	}
	// An ordinary time.
	got = Instant(time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC), lon)
	if got.UTC().Format(time.RFC3339) != "2026-07-01T08:00:00Z" {
		t.Fatalf("summer: %s", got.UTC())
	}
}

func TestWindowsZone(t *testing.T) {
	l, err := Location("W. Europe Standard Time")
	if err != nil || l.String() != "Europe/Berlin" {
		t.Fatalf("%v %v", l, err)
	}
	if id, ok := IANA("Pacific Standard Time"); !ok || id != "America/Los_Angeles" {
		t.Fatal(id)
	}
	if _, err := Location("Nowhere/Atlantis"); !errors.Is(err, ErrZone) {
		t.Fatal(err)
	}
}

func starts(os []Occurrence) []string {
	out := make([]string, len(os))
	for i, o := range os {
		out[i] = o.Local
	}
	return out
}

func TestWeeklyAcrossDST(t *testing.T) {
	os, err := Expand(ExpandArgs{
		Start:      Zoned{Local: "2026-03-17T09:00:00", Zone: "Europe/London"},
		Duration:   time.Hour,
		Recurrence: Recurrence{RRule: "FREQ=WEEKLY;COUNT=4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range os {
		if !strings.HasSuffix(o.Local, "T09:00:00") {
			t.Fatalf("not 09:00 local: %+v", os)
		}
	}
	// Before the change 09:00 is 09:00Z, after it 08:00Z.
	if os[1].Start != "2026-03-24T09:00:00Z" || os[2].Start != "2026-03-31T08:00:00Z" {
		t.Fatalf("%+v", os)
	}
	// October too.
	os, err = Expand(ExpandArgs{Start: Zoned{Local: "2026-10-20T09:00:00", Zone: "Europe/London"}, Duration: time.Hour,
		Recurrence: Recurrence{RRule: "RRULE:FREQ=WEEKLY;COUNT=2"}})
	if err != nil || os[0].Start != "2026-10-20T08:00:00Z" || os[1].Start != "2026-10-27T09:00:00Z" {
		t.Fatalf("%+v %v", os, err)
	}
}

func TestNewYorkFromLondonWindow(t *testing.T) {
	// A New York 09:00 daily series seen in a London window spanning the
	// three weeks when the two zones' changes differ.
	w := Window{From: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)}
	os, err := Expand(ExpandArgs{Start: Zoned{Local: "2026-01-05T09:00:00", Zone: "America/New_York"}, Duration: 30 * time.Minute,
		Recurrence: Recurrence{RRule: "FREQ=WEEKLY;BYDAY=MO"}, Window: w})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range os {
		if !strings.HasSuffix(o.Local, "T09:00:00") {
			t.Fatalf("%+v", o)
		}
	}
	if os[0].Start != "2026-03-02T14:00:00Z" || os[2].Start != "2026-03-16T13:00:00Z" {
		t.Fatalf("%+v", os)
	}
	if len(os) != 5 {
		t.Fatalf("want 5 Mondays, got %v", starts(os))
	}
}

// RFC 5545 section 3.8.5.3's examples, a few of them.
func TestRFCExamples(t *testing.T) {
	ny := "America/New_York"
	cases := []struct {
		name, start, rule string
		window            Window
		want              []string
	}{
		{"daily for 10", "1997-09-02T09:00:00", "FREQ=DAILY;COUNT=10", Window{}, []string{"1997-09-02T09:00:00", "1997-09-03T09:00:00", "1997-09-04T09:00:00", "1997-09-05T09:00:00", "1997-09-06T09:00:00", "1997-09-07T09:00:00", "1997-09-08T09:00:00", "1997-09-09T09:00:00", "1997-09-10T09:00:00", "1997-09-11T09:00:00"}},
		{"every other day, 5", "1997-09-02T09:00:00", "FREQ=DAILY;INTERVAL=2;COUNT=5", Window{}, []string{"1997-09-02T09:00:00", "1997-09-04T09:00:00", "1997-09-06T09:00:00", "1997-09-08T09:00:00", "1997-09-10T09:00:00"}},
		{"weekly until", "1997-09-02T09:00:00", "FREQ=WEEKLY;UNTIL=19970930T000000Z", Window{}, []string{"1997-09-02T09:00:00", "1997-09-09T09:00:00", "1997-09-16T09:00:00", "1997-09-23T09:00:00"}},
		{"every other week Tu,Th, 8", "1997-09-02T09:00:00", "FREQ=WEEKLY;INTERVAL=2;COUNT=8;WKST=SU;BYDAY=TU,TH", Window{}, []string{"1997-09-02T09:00:00", "1997-09-04T09:00:00", "1997-09-16T09:00:00", "1997-09-18T09:00:00", "1997-09-30T09:00:00", "1997-10-02T09:00:00", "1997-10-14T09:00:00", "1997-10-16T09:00:00"}},
		{"first Friday, 4", "1997-09-05T09:00:00", "FREQ=MONTHLY;COUNT=4;BYDAY=1FR", Window{}, []string{"1997-09-05T09:00:00", "1997-10-03T09:00:00", "1997-11-07T09:00:00", "1997-12-05T09:00:00"}},
		{"last weekday of month", "1997-09-30T09:00:00", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1;COUNT=3", Window{}, []string{"1997-09-30T09:00:00", "1997-10-31T09:00:00", "1997-11-28T09:00:00"}},
		{"third to last day", "1997-09-28T09:00:00", "FREQ=MONTHLY;BYMONTHDAY=-3;COUNT=3", Window{}, []string{"1997-09-28T09:00:00", "1997-10-29T09:00:00", "1997-11-28T09:00:00"}},
		{"yearly June and July", "1997-06-10T09:00:00", "FREQ=YEARLY;COUNT=4;BYMONTH=6,7", Window{}, []string{"1997-06-10T09:00:00", "1997-07-10T09:00:00", "1998-06-10T09:00:00", "1998-07-10T09:00:00"}},
	}
	for _, c := range cases {
		os, err := Expand(ExpandArgs{Start: Zoned{Local: c.start, Zone: ny}, Duration: time.Hour, Recurrence: Recurrence{RRule: c.rule}, Window: c.window})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := strings.Join(starts(os), " "); got != strings.Join(c.want, " ") {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, strings.Join(c.want, " "))
		}
	}
}

func TestExDateRDateAndCap(t *testing.T) {
	os, err := Expand(ExpandArgs{Start: Zoned{Local: "2026-05-04T10:00:00", Zone: "Europe/Paris"}, Duration: time.Hour,
		Recurrence: Recurrence{RRule: "FREQ=DAILY;COUNT=4", ExDate: []string{"2026-05-05T10:00:00"}, RDate: []string{"2026-05-10T15:00:00"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "2026-05-04T10:00:00 2026-05-06T10:00:00 2026-05-07T10:00:00 2026-05-10T15:00:00"
	if got := strings.Join(starts(os), " "); got != want {
		t.Fatalf("got %s", got)
	}
	// An instant EXDATE removes the same occurrence.
	os, _ = Expand(ExpandArgs{Start: Zoned{Local: "2026-05-04T10:00:00", Zone: "Europe/Paris"}, Duration: time.Hour,
		Recurrence: Recurrence{RRule: "FREQ=DAILY;COUNT=2", ExDate: []string{"2026-05-05T08:00:00Z"}}})
	if len(os) != 1 {
		t.Fatalf("%+v", os)
	}
	if _, err := Expand(ExpandArgs{Start: Zoned{Local: "2026-05-04T10:00:00", Zone: "UTC"}, Recurrence: Recurrence{RRule: "FREQ=DAILY"}}); !errors.Is(err, ErrUnbounded) {
		t.Fatalf("unbounded: %v", err)
	}
	w := Window{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	if _, err := Expand(ExpandArgs{Start: Zoned{Local: "2026-01-01T00:00:00", Zone: "UTC"}, Recurrence: Recurrence{RRule: "FREQ=MINUTELY"}, Window: w}); !errors.Is(err, ErrTooMany) {
		t.Fatalf("runaway: %v", err)
	}
	// An event with no rule is one occurrence, kept when it overlaps the window.
	os, err = Expand(ExpandArgs{Start: Zoned{Local: "2026-05-04T23:30:00", Zone: "UTC"}, Duration: time.Hour,
		Window: Window{From: time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}})
	if err != nil || len(os) != 1 {
		t.Fatalf("%+v %v", os, err)
	}
}

func TestAllDay(t *testing.T) {
	os, err := Expand(ExpandArgs{Start: Zoned{Date: "2026-12-24"}, Duration: 24 * time.Hour, Recurrence: Recurrence{RRule: "FREQ=YEARLY;COUNT=2", ExDate: []string{"2027-12-24"}}})
	if err != nil || len(os) != 1 || os[0].Date != "2026-12-24" || !os[0].AllDay || os[0].RecurrenceID != "2026-12-24" {
		t.Fatalf("%+v %v", os, err)
	}
}

func TestSplitRule(t *testing.T) {
	start := Zoned{Local: "2026-06-01T09:00:00", Zone: "Europe/London"}
	s, err := SplitRule(start, "FREQ=WEEKLY;BYDAY=MO", "2026-06-15T08:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if s.Before != "FREQ=WEEKLY;UNTIL=20260615T075959Z;BYDAY=MO" || s.After != "FREQ=WEEKLY;BYDAY=MO" || s.AfterStart.Local != "2026-06-15T09:00:00" {
		t.Fatalf("%+v", s)
	}
	first, _ := Expand(ExpandArgs{Start: start, Recurrence: Recurrence{RRule: s.Before}})
	if len(first) != 2 {
		t.Fatalf("before keeps %v", starts(first))
	}
	s, err = SplitRule(start, "FREQ=WEEKLY;COUNT=5", "2026-06-15T08:00:00Z")
	if err != nil || s.Before != "FREQ=WEEKLY;COUNT=2" || s.After != "FREQ=WEEKLY;COUNT=3" {
		t.Fatalf("%+v %v", s, err)
	}
	s, err = SplitRule(start, "FREQ=WEEKLY;COUNT=5", "2026-06-01T08:00:00Z")
	if err != nil || !s.BeforeEmpty {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestDescribeRule(t *testing.T) {
	for rule, want := range map[string]string{
		"":           "does not repeat",
		"FREQ=DAILY": "every day",
		"FREQ=WEEKLY;INTERVAL=2;BYDAY=TU;UNTIL=20260630T235959Z": "every other Tuesday until 30 June 2026",
		"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR":                       "every weekday",
		"FREQ=MONTHLY;BYDAY=1FR;COUNT=4":                         "every month on the first Friday 4 times",
		"FREQ=MONTHLY;BYMONTHDAY=15":                             "every month on the 15th",
		"FREQ=YEARLY;BYMONTH=6,7":                                "every year in June and July",
		"FREQ=WEEKLY;INTERVAL=3;BYDAY=MO,WE":                     "every third Monday and Wednesday",
		"FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1":          "every month on the last of Monday, Tuesday, Wednesday, Thursday and Friday",
	} {
		got, err := DescribeRule(rule, "UTC")
		if err != nil || got != want {
			t.Errorf("%s: got %q (%v), want %q", rule, got, err, want)
		}
	}
}

func TestFreeSlotsThreeZones(t *testing.T) {
	w := Window{From: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)}
	lon := DefaultWorkingHours("Europe/London")   // 08:00–16:00Z
	ny := DefaultWorkingHours("America/New_York") // 13:00–21:00Z
	ber := DefaultWorkingHours("Europe/Berlin")   // 07:00–15:00Z
	busyNY := []Interval{{Start: time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC)}}
	slots, err := FreeSlots(SlotArgs{People: []Person{{ID: "anna", Hours: lon}, {ID: "ben", Hours: ny, Busy: busyNY}, {ID: "carl", Hours: ber}},
		Duration: 30 * time.Minute, Window: w, Count: 3, Spread: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 3 {
		t.Fatalf("%+v", slots)
	}
	for _, s := range slots {
		st, _ := time.Parse(time.RFC3339, s.Start)
		// Everyone's hours overlap only 13:00 to 15:00 UTC.
		if st.Hour() < 13 || st.Hour() >= 15 || (st.Day() == 1 && st.Before(time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC))) {
			t.Fatalf("slot outside everyone's free hours: %+v", s)
		}
		if len(s.Local) != 3 || s.Local[1].Zone != "America/New_York" {
			t.Fatalf("%+v", s.Local)
		}
	}
	// A buffer after keeps a slot off the end of a busy block.
	slots, _ = FreeSlots(SlotArgs{People: []Person{{ID: "ben", Hours: ny, Busy: busyNY}}, Duration: time.Hour, Before: 15 * time.Minute,
		Window: Window{From: time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC), To: time.Date(2026, 6, 1, 16, 0, 0, 0, time.UTC)}, Count: 5})
	for _, s := range slots {
		if s.Start == "2026-06-01T14:30:00Z" {
			t.Fatalf("the buffer was ignored: %+v", slots)
		}
	}
}

func TestBusy(t *testing.T) {
	iv, err := Busy([]BusyEvent{
		{Start: "2026-06-01T09:00:00Z", End: "2026-06-01T10:00:00Z"},
		{Start: "2026-06-01T09:30:00Z", End: "2026-06-01T11:00:00Z"},
		{Start: "2026-06-01T12:00:00Z", End: "2026-06-01T13:00:00Z", Transparency: "transparent"},
		{Start: "2026-06-01T14:00:00Z", End: "2026-06-01T15:00:00Z", Response: "declined"},
	})
	if err != nil || len(iv) != 1 || iv[0].End.Hour() != 11 {
		t.Fatalf("%+v %v", iv, err)
	}
}
