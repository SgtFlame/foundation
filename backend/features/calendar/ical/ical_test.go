package ical

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
)

func TestMain(m *testing.M) {
	if ChildRequested() {
		ServeChild()
		return
	}
	os.Exit(m.Run())
}

func fixture(t *testing.T, name string) Parsed {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return p
}

func TestGoogleRequest(t *testing.T) {
	p := fixture(t, "google-request.ics")
	if p.Method != "REQUEST" || len(p.Events) != 1 {
		t.Fatalf("%+v", p)
	}
	e := p.Events[0]
	if e.Title != "Renewal, next steps" || e.Start != (when.Zoned{Local: "2026-12-03T10:00:00", Zone: "America/New_York"}) || e.End.Local != "2026-12-03T11:00:00" {
		t.Fatalf("%+v", e)
	}
	if e.VideoLink != "https://meet.google.com/abc-defg-hij" || e.Organizer.Email != "dana@example.com" || len(e.Attendees) != 2 {
		t.Fatalf("%+v", e)
	}
	if e.Attendees[1].Email != "tony@example.org" || e.Attendees[1].Response != calendar.NeedsAction || !e.Attendees[0].Organizer {
		t.Fatalf("%+v", e.Attendees)
	}
	if !strings.Contains(e.Description, "Renewal call, as discussed.\nJoin") {
		t.Fatalf("%q", e.Description)
	}
}

func TestOutlookSeriesWithException(t *testing.T) {
	p := fixture(t, "outlook-series.ics")
	if len(p.Events) != 2 || len(p.Problems) != 0 {
		t.Fatalf("%+v", p)
	}
	master, ex := p.Events[0], p.Events[1]
	if master.Start.Zone != "Europe/Berlin" || master.Recurrence == nil || !strings.HasPrefix(master.Recurrence.RRule, "FREQ=WEEKLY") {
		t.Fatalf("master: %+v", master)
	}
	if len(master.Recurrence.ExDate) != 1 || master.Recurrence.ExDate[0] != "2026-12-29T08:00:00Z" {
		t.Fatalf("exdate: %+v", master.Recurrence)
	}
	if master.VideoLink == "" || master.DescriptionHTML == "" || master.Sequence != 2 || !master.Attendees[1].Optional || master.Attendees[0].Email != "tony@example.org" {
		t.Fatalf("%+v", master)
	}
	if ex.RecurrenceID != "2026-12-15T08:00:00Z" || ex.SeriesID != master.ICalUID || ex.Start.Local != "2026-12-15T14:00:00" || ex.ID == master.ID {
		t.Fatalf("exception: %+v", ex)
	}
	// The series expands without its excluded week.
	os, err := when.Expand(when.ExpandArgs{Start: master.Start, Duration: 30 * time.Minute, Recurrence: *master.Recurrence,
		Window: when.Window{From: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 6, 0, 0, 0, 0, time.UTC)}})
	if err != nil || len(os) != 5 {
		t.Fatalf("%d occurrences %v", len(os), err)
	}
}

func TestAppleAllDayAndFastmail(t *testing.T) {
	e := fixture(t, "apple-allday.ics").Events[0]
	if e.Start.Date != "2026-12-25" || e.End.Date != "2026-12-26" || e.Transparency != calendar.Transparent || e.Recurrence.RRule != "FREQ=YEARLY" {
		t.Fatalf("%+v", e)
	}
	p := fixture(t, "fastmail-publish.ics")
	f := p.Events[0]
	if f.Start.Zone != "Australia/Sydney" || f.End.Local != "2026-12-05T20:00:00" || f.Title != "Dinner; at Luca" || f.Location != "Luca, Surry Hills" {
		t.Fatalf("%+v", f)
	}
	fb := p.FreeBusy[0]
	if fb.Who != "sam@fastmail.com" || len(fb.Busy) != 3 || fb.Busy[1].End != "2026-12-03T09:30:00Z" || fb.Busy[2].Type != "tentative" {
		t.Fatalf("%+v", fb)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"google-request.ics", "outlook-series.ics", "apple-allday.ics", "fastmail-publish.ics"} {
		p := fixture(t, name)
		text, err := Compose(MethodPublish, p.Events, ComposeOptions{Now: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, line := range strings.Split(text, "\r\n") {
			if len(line) > 75 {
				t.Fatalf("%s: a line of %d octets: %q", name, len(line), line)
			}
		}
		back, err := Parse([]byte(text))
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, text)
		}
		if len(back.Events) != len(p.Events) {
			t.Fatalf("%s: %d events back", name, len(back.Events))
		}
		for i := range p.Events {
			a, b := p.Events[i], back.Events[i]
			if a.Title != b.Title || a.Start != b.Start || a.End != b.End || a.RecurrenceID != b.RecurrenceID || a.Location != b.Location || a.Description != b.Description || len(a.Attendees) != len(b.Attendees) {
				t.Fatalf("%s: event %d changed:\n%+v\n%+v", name, i, a, b)
			}
			if (a.Recurrence == nil) != (b.Recurrence == nil) || a.Recurrence != nil && (a.Recurrence.RRule != b.Recurrence.RRule || strings.Join(a.Recurrence.ExDate, ",") != strings.Join(b.Recurrence.ExDate, ",")) {
				t.Fatalf("%s: recurrence changed %+v %+v", name, a.Recurrence, b.Recurrence)
			}
		}
		if strings.Contains(text, "Europe/Berlin") && !strings.Contains(text, "BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin") {
			t.Fatalf("%s: no VTIMEZONE for Berlin", name)
		}
	}
}

func TestReplyAndApply(t *testing.T) {
	inv := fixture(t, "google-request.ics").Events[0]
	r, err := Reply(inv, "Tony@Example.org", "accepted", "See you then")
	if err != nil {
		t.Fatal(err)
	}
	text, err := Compose(MethodReply, []calendar.Event{r}, ComposeOptions{})
	if err != nil || !strings.Contains(text, "METHOD:REPLY") || !strings.Contains(text, "PARTSTAT=ACCEPTED") {
		t.Fatalf("%v\n%s", err, text)
	}
	back, _ := Parse([]byte(text))
	updated, err := Apply(inv, back.Method, back.Events[0])
	if err != nil {
		t.Fatal(err)
	}
	if updated.Attendees[1].Response != calendar.Accepted || updated.Attendees[0].Response != calendar.Accepted || inv.Attendees[1].Response != calendar.NeedsAction {
		t.Fatalf("%+v", updated.Attendees)
	}
	// An older SEQUENCE is refused.
	cur := inv
	cur.Sequence = 3
	if _, err := Apply(cur, MethodReply, back.Events[0]); !errors.Is(err, ErrOlder) {
		t.Fatalf("older: %v", err)
	}
	// A reply from someone not invited is refused.
	stranger, _ := Reply(inv, "eve@example.net", "accepted", "")
	if _, err := Apply(inv, MethodReply, stranger); !errors.Is(err, ErrNotInvited) {
		t.Fatalf("stranger: %v", err)
	}
	// A new REQUEST keeps answers given at the same sequence.
	again, _ := Apply(updated, MethodRequest, inv)
	if again.Attendees[1].Response != calendar.Accepted {
		t.Fatalf("%+v", again.Attendees)
	}
	if c, _ := Apply(updated, MethodCancel, inv); c.Status != calendar.StatusCancelled {
		t.Fatal(c.Status)
	}
}

func TestComposeEscapingAndFolding(t *testing.T) {
	e := calendar.Event{ICalUID: "x@axiom", Title: "Plan; budget, Q1 \\ review", Start: when.Zoned{Local: "2026-03-29T09:00:00", Zone: "Europe/London"},
		End: when.Zoned{Local: "2026-03-29T10:00:00", Zone: "Europe/London"}, Description: strings.Repeat("Ünïcödé ", 30) + "\nend"}
	text, err := Compose(MethodRequest, []calendar.Event{e}, ComposeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `SUMMARY:Plan\; budget\, Q1 \\ review`) || !strings.Contains(text, "BEGIN:DAYLIGHT") {
		t.Fatal(text)
	}
	back, err := Parse([]byte(text))
	if err != nil || back.Events[0].Description != e.Description || back.Events[0].Title != e.Title {
		t.Fatalf("%v %+v", err, back.Events)
	}
}

func TestChild(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	read := Child(exe)
	raw, _ := os.ReadFile("testdata/outlook-series.ics")
	p, err := read(context.Background(), raw)
	if err != nil || len(p.Events) != 2 {
		t.Fatalf("%v %+v", err, p)
	}
	// Too large, refused before a child starts.
	if _, err := read(context.Background(), make([]byte, 50<<20)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large: %v", err)
	}
	// Deeply nested components and a never-ending line: refused, and the
	// parent goes on.
	nested := "BEGIN:VCALENDAR\r\n" + strings.Repeat("BEGIN:X\r\n", 200000)
	if _, err := read(context.Background(), []byte(nested[:min(len(nested), MaxBytes)])); err == nil {
		t.Fatal("nested: no error")
	}
	long := "BEGIN:VCALENDAR\r\nX:" + strings.Repeat("a", MaxBytes-40)
	if _, err := read(context.Background(), []byte(long)); err == nil {
		t.Fatal("long line: no error")
	}
	// Not a calendar.
	if _, err := read(context.Background(), []byte("hello")); err == nil || !strings.Contains(err.Error(), "not an iCalendar") {
		t.Fatalf("not ics: %v", err)
	}
	// Two at once, a third waits.
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := read(context.Background(), raw)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
