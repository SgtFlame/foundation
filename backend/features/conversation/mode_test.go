package conversation

import "testing"

// A focus's kind ending in * matches every kind it begins (forge 7p-6).
func TestFocusMatchesKindPatterns(t *testing.T) {
	f := &Focus{Kinds: []string{"prefab", "app:*"}, Subjects: []string{"graph"}, OfferOnly: true}
	for _, c := range []struct {
		kind, subject string
		want          bool
	}{
		{"prefab", "", true},
		{"app:mail:main", "", true},
		{"app:mail:thread", "record", true},
		{"app", "", false},
		{"markdown", "graph", true},
		{"markdown", "note", false},
		{"", "", false},
	} {
		if got := f.Matches(c.kind, c.subject); got != c.want {
			t.Errorf("Matches(%q, %q) = %v", c.kind, c.subject, got)
		}
	}
	if !(*Focus)(nil).Matches("anything", "") {
		t.Error("no focus is offered everywhere")
	}
}
