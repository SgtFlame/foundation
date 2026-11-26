package collabservice

import (
	"strings"
	"testing"
)

func TestApplyEdits(t *testing.T) {
	got, err := ApplyEdits("a b c", []Edit{{Find: "b", Replace: "B"}, {Find: "B c", Replace: "x"}})
	if err != nil || got != "a x" {
		t.Fatalf("ApplyEdits = %q, %v", got, err)
	}
	for _, c := range []struct {
		edits []Edit
		want  string
	}{
		{[]Edit{{Find: "z"}}, "not in the file"},
		{[]Edit{{Find: "a"}}, "occurs 2 times"},
		{[]Edit{{Find: ""}}, "nothing to find"},
	} {
		if _, err := ApplyEdits("a a", c.edits); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ApplyEdits(%v) = %v, want %q", c.edits, err, c.want)
		}
	}
}

func TestPrepare(t *testing.T) {
	at := func(n int) *int { return &n }
	// A range in a text where its words recur grows until it is unique
	text := "# Rules\n\nA table holds four.\n\n# Times\n\nA table holds four at lunch.\n"
	start := strings.Index(text, "four.")
	p := Prepare(text, []Edit{{Start: at(start), End: at(start + 4), Find: "four", Replace: "six"}})
	if !p.Applied[0] || p.Text != strings.Replace(text, "holds four.", "holds six.", 1) {
		t.Fatalf("Prepare = %+v", p)
	}
	if strings.Count(text, p.Edits[0].Find) != 1 {
		t.Fatalf("find %q is not unique", p.Edits[0].Find)
	}
	// An insertion at a place
	end := len([]rune(text))
	p = Prepare(text, []Edit{{Start: at(end), End: at(end), Replace: "\n# Deposits\n"}})
	if !p.Applied[0] || !strings.HasSuffix(p.Text, "at lunch.\n\n# Deposits\n") {
		t.Fatalf("insertion = %+v", p)
	}
	// Runes, not bytes
	p = Prepare("café au lait, café noir", []Edit{{Start: at(14), End: at(18), Replace: "thé"}})
	if p.Text != "café au lait, thé noir" {
		t.Fatalf("runes = %q", p.Text)
	}
	// Each edit is decided against what the ones before left; the ones that
	// cannot apply are skipped with their reason
	p = Prepare("one two three", []Edit{{Find: "two", Replace: "2"}, {Find: "four"}, {Start: at(4), End: at(5), Find: "x"}, {Start: at(0), End: at(99)}, {Find: "three", Replace: "3"}})
	if p.Text != "one 2 3" || len(p.Edits) != 2 {
		t.Fatalf("partial = %+v", p)
	}
	want := []string{"", "not in the file", "has changed", "outside the text", ""}
	for i, w := range want {
		if (w == "") != p.Applied[i] || !strings.Contains(p.Problems[i], w) {
			t.Errorf("edit %d: applied %v, problem %q, want %q", i+1, p.Applied[i], p.Problems[i], w)
		}
	}
	if p := Prepare("", []Edit{{Start: at(0), End: at(0), Replace: "x"}}); p.Applied[0] {
		t.Fatalf("an empty text took a range: %+v", p)
	}
}
