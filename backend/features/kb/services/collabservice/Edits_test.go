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
