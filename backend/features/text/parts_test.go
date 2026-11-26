package text

import (
	"slices"
	"strings"
	"testing"
)

// Parts are each heading's own part with its path and lines (forge 18f).
func TestParts(t *testing.T) {
	s := "Intro line.\n\n# Booking\n\nA table holds four.\n\n## Deposits\n\nTen euros.\n\n```\n# not a heading\n```\n\n# Empty\n\n"
	ps := Parts(s, 0)
	if len(ps) != 4 {
		t.Fatalf("parts = %+v", ps)
	}
	if ps[0].Heading != "" || len(ps[0].Path) != 0 || ps[0].Text != "Intro line." || ps[0].StartLine != 1 || ps[0].EndLine != 1 {
		t.Errorf("intro = %+v", ps[0])
	}
	if !slices.Equal(ps[1].Path, []string{"Booking"}) || ps[1].StartLine != 3 || ps[1].EndLine != 5 || !strings.Contains(ps[1].Text, "holds four") {
		t.Errorf("booking = %+v", ps[1])
	}
	if !slices.Equal(ps[2].Path, []string{"Booking", "Deposits"}) || ps[2].Heading != "Deposits" || !strings.Contains(ps[2].Text, "# not a heading") || ps[2].EndLine != 13 {
		t.Errorf("deposits = %+v", ps[2])
	}
	if ps[3].Heading != "Empty" || ps[3].Text != "# Empty" {
		t.Errorf("empty = %+v", ps[3])
	}
	long := "# Rules\n\n" + strings.Repeat("a", 30) + "\n\n" + strings.Repeat("b", 30) + "\n\n" + strings.Repeat("c", 30) + "\n"
	cutUp := Parts(long, 50)
	if len(cutUp) != 3 || !slices.Equal(cutUp[2].Path, []string{"Rules"}) || cutUp[2].Text != strings.Repeat("c", 30) || cutUp[2].StartLine != 7 {
		t.Errorf("cut = %+v", cutUp)
	}
	if got := Parts("   \n", 0); len(got) != 0 {
		t.Errorf("blank = %+v", got)
	}
}
