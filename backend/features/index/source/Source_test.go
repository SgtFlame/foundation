package indexsource

import "testing"

func TestTitle(t *testing.T) {
	for _, c := range []struct{ path, heading, want string }{
		{"veritas/notes.md", "", "veritas/notes"},
		{"veritas/notes.md", "Guide > Install", "veritas/notes › Guide › Install"},
		{"top.md", "Intro", "top › Intro"},
	} {
		if got := title(c.path, c.heading); got != c.want {
			t.Errorf("title(%q, %q) = %q, want %q", c.path, c.heading, got, c.want)
		}
	}
}
