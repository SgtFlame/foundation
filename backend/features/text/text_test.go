package text

import (
	"slices"
	"strings"
	"testing"
)

const board = `# Table booking

People book a table online.

## Booking

- A guest picks a date and a time.
- Bookings of more than 8 people need a call.

## Payments

A deposit of 10 euros is taken.

## Cancelling

A guest may cancel until 2 hours before.
`

func TestDiffHeadingsAndLines(t *testing.T) {
	b := strings.Replace(board, "more than 8 people", "more than 10 people", 1)
	b = strings.Replace(b, "## Cancelling", "## Reminders\n\nA reminder is sent the day before.\n\n## Cancelling", 1)
	hs := Diff(board, b, Line)
	if len(hs) != 2 {
		t.Fatalf("hunks %+v", hs)
	}
	if hs[0].Op != OpReplace || !slices.Equal(hs[0].HeadingPath, []string{"Table booking", "Booking"}) || !strings.Contains(hs[0].After, "10 people") || !strings.Contains(hs[0].Before, "8 people") {
		t.Errorf("first %+v", hs[0])
	}
	if hs[0].ALines.Start != 8 || hs[0].BLines != hs[0].ALines {
		t.Errorf("lines %+v %+v", hs[0].ALines, hs[0].BLines)
	}
	if hs[1].Op != OpInsert || !slices.Equal(hs[1].HeadingPath, []string{"Table booking", "Reminders"}) {
		t.Errorf("second %+v", hs[1])
	}
	// Spans give the texts back.
	ra, rb := []rune(board), []rune(b)
	for _, h := range hs {
		if string(ra[h.A.Start:h.A.End]) != h.Before || string(rb[h.B.Start:h.B.End]) != h.After {
			t.Errorf("spans %+v", h)
		}
	}
	// Applying the hunks to a gives b.
	if got := apply(board, hs); got != b {
		t.Errorf("applied:\n%s", got)
	}
	if hs := Diff(board, board, Line); len(hs) != 0 {
		t.Errorf("no change gave %+v", hs)
	}
	// From nothing: one insert under no heading.
	hs = Diff("", board, Line)
	if len(hs) != 1 || hs[0].Op != OpInsert || hs[0].After != board || !slices.Equal(hs[0].HeadingPath, []string{"Table booking"}) {
		t.Errorf("from empty %+v", hs)
	}
	// A deletion's path is the old text's.
	gone := strings.Replace(board, "A deposit of 10 euros is taken.\n", "", 1)
	hs = Diff(board, gone, Line)
	if len(hs) != 1 || hs[0].Op != OpDelete || !slices.Equal(hs[0].HeadingPath, []string{"Table booking", "Payments"}) {
		t.Errorf("delete %+v", hs)
	}
}

func TestDiffMovedParagraph(t *testing.T) {
	a := "# A\n\nFirst paragraph here.\n\nSecond paragraph here.\n\n# B\n\nThird.\n"
	b := "# A\n\nSecond paragraph here.\n\n# B\n\nThird.\n\nFirst paragraph here.\n"
	hs := Diff(a, b, Line)
	var moved []Hunk
	for _, h := range hs {
		if h.Op == OpMove {
			moved = append(moved, h)
		}
	}
	if len(moved) != 1 || strings.TrimSpace(moved[0].Before) != "First paragraph here." || strings.TrimSpace(moved[0].After) != "First paragraph here." ||
		!slices.Equal(moved[0].HeadingPath, []string{"B"}) {
		t.Fatalf("moved %+v (all %+v)", moved, hs)
	}
}

func TestDiffWords(t *testing.T) {
	a := "Bookings of more than 8 people need a phone call."
	b := "Bookings of more than 10 guests need a call."
	hs := Diff(a, b, Word)
	if len(hs) != 2 {
		t.Fatalf("hunks %+v", hs)
	}
	if hs[0].Op != OpReplace || hs[0].Before != "8 people" || hs[0].After != "10 guests" {
		t.Errorf("first %+v", hs[0])
	}
	if hs[1].Op != OpDelete || strings.TrimSpace(hs[1].Before) != "phone" {
		t.Errorf("second %+v", hs[1])
	}
	if got := apply(a, hs); got != b {
		t.Errorf("applied %q", got)
	}
}

func TestDiffLongTexts(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 3000; i++ {
		a.WriteString("line " + strings.Repeat("x", i%7) + "\n")
		if i%100 == 0 {
			b.WriteString("changed\n")
		} else {
			b.WriteString("line " + strings.Repeat("x", i%7) + "\n")
		}
	}
	hs := Diff(a.String(), b.String(), Line)
	if got := apply(a.String(), hs); got != b.String() {
		t.Fatalf("long diff does not apply (%d hunks)", len(hs))
	}
}

// apply makes the new text from the old and the hunks (none a move).
func apply(a string, hs []Hunk) string {
	ra := []rune(a)
	var out strings.Builder
	pos := 0
	for _, h := range hs {
		if h.Op == OpMove {
			panic("apply does not take moves")
		}
		out.WriteString(string(ra[pos:h.A.Start]))
		out.WriteString(h.After)
		pos = h.A.End
	}
	out.WriteString(string(ra[pos:]))
	return out.String()
}

func TestHeadingsSkipCode(t *testing.T) {
	s := "# One\n\n```\n# not a heading\n```\n\n## Two ##\n\n#nospace\n"
	hs := Headings(s)
	if len(hs) != 2 || hs[1].Title != "Two" || !slices.Equal(hs[1].Path, []string{"One", "Two"}) {
		t.Fatalf("headings %+v", hs)
	}
	sec, ok := SectionOf(board, []string{"Table booking", "Payments"})
	if !ok || !strings.HasPrefix(sec.Text, "## Payments") || strings.Contains(sec.Text, "Cancelling") {
		t.Errorf("section %+v", sec)
	}
	if sec, ok := SectionOf(board, []string{"payments"}); !ok || !strings.HasPrefix(sec.Text, "## Payments") {
		t.Errorf("by title %+v", sec)
	}
}

func TestChangedSections(t *testing.T) {
	b := strings.Replace(board, "10 euros", "20 euros", 1)
	hs := Diff(board, b, Line)
	cs := ChangedSections(board, b, hs)
	if len(cs) != 1 || !slices.Equal(cs[0]["headingPath"].([]string), []string{"Table booking", "Payments"}) ||
		!strings.Contains(cs[0]["a"].(string), "10 euros") || !strings.Contains(cs[0]["b"].(string), "20 euros") || strings.Contains(cs[0]["b"].(string), "Cancelling") {
		t.Fatalf("sections %+v", cs)
	}
	// A new section: only the new text has it; the intro before any
	// heading has the empty path
	b = "Intro changed.\n" + strings.Replace(board, "## Cancelling", "## Reminders\n\nSent the day before.\n\n## Cancelling", 1)
	cs = ChangedSections(board, b, Diff(board, b, Line))
	if len(cs) != 2 || len(cs[0]["headingPath"].([]string)) != 0 || cs[0]["a"] != "" ||
		!slices.Equal(cs[1]["headingPath"].([]string), []string{"Table booking", "Reminders"}) || cs[1]["a"] != "" || !strings.HasPrefix(cs[1]["b"].(string), "## Reminders") {
		t.Fatalf("new sections %+v", cs)
	}
}

func TestResolveAnchors(t *testing.T) {
	// Exact, under its heading.
	a := Resolve(board, "A deposit of 10 euros is taken.", []string{"Table booking", "Payments"}, "")
	if !a.Found || !a.Exact || string([]rune(board)[a.Start:a.End]) != "A deposit of 10 euros is taken." || !slices.Equal(a.HeadingPath, []string{"Table booking", "Payments"}) {
		t.Errorf("exact %+v", a)
	}
	// Moved to another section: found anywhere, its new heading given.
	moved := strings.Replace(board, "A deposit of 10 euros is taken.\n", "", 1) + "\nA deposit of 10 euros is taken.\n"
	a = Resolve(moved, "A deposit of 10 euros is taken.", []string{"Table booking", "Payments"}, "")
	if !a.Found || !a.Exact || !slices.Equal(a.HeadingPath, []string{"Table booking", "Cancelling"}) {
		t.Errorf("moved %+v", a)
	}
	// Spaces and case differ.
	a = Resolve(board, "a deposit   of 10 EUROS", nil, "")
	if !a.Found || a.Exact || string([]rune(board)[a.Start:a.End]) != "A deposit of 10 euros" {
		t.Errorf("normalized %+v %q", a, string([]rune(board)[a.Start:a.End]))
	}
	// Edited since: a few words differ.
	edited := strings.Replace(board, "A guest may cancel until 2 hours before.", "A guest may cancel online until 3 hours before.", 1)
	a = Resolve(edited, "A guest may cancel until 2 hours before.", []string{"Cancelling"}, "")
	if !a.Found || a.Exact || a.Score < MinScore || !strings.Contains(string([]rune(edited)[a.Start:a.End]), "cancel online until 3 hours") {
		t.Errorf("edited %+v %q", a, string([]rune(edited)[a.Start:a.End]))
	}
	// Missing.
	if a := Resolve(board, "Refunds are paid within a week to the card used.", nil, ""); a.Found {
		t.Errorf("missing found %+v", a)
	}
	if a := Resolve(board, "  ", nil, ""); a.Found {
		t.Errorf("empty found %+v", a)
	}
	// Twice: the context picks.
	twice := "# One\n\nThe fee is paid.\nAbout tables.\n\n# Two\n\nAbout rooms.\nThe fee is paid.\n"
	a = Resolve(twice, "The fee is paid.", nil, "About rooms.")
	if !a.Found || !slices.Equal(a.HeadingPath, []string{"Two"}) {
		t.Errorf("context %+v", a)
	}
	a = Resolve(twice, "The fee is paid.", []string{"Two"}, "")
	if !a.Found || !slices.Equal(a.HeadingPath, []string{"Two"}) {
		t.Errorf("heading %+v", a)
	}
}
