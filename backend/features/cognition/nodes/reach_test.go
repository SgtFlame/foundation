package cognitionnodes

import (
	"encoding/json"
	"testing"
)

// Forge 7p-3: Call graph's Reach takes a subject, a list of them or keys,
// and gives the space-separated keys features.Principal holds.
func TestReachOf(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"", ""},
		{[]any{}, ""},
		{map[string]any{"entity": "mailThread", "id": "t1"}, "mailThread:t1"},
		{[]any{map[string]any{"entity": "mailThread", "id": "t1"}, "mailThread:t2"}, "mailThread:t1 mailThread:t2"},
		{json.RawMessage(`[{"entity":"demoTicket","id":42}]`), "demoTicket:42"},
		{"  mailThread:t1   mailThread:t2 ", "mailThread:t1 mailThread:t2"},
	} {
		got, err := ReachOf(c.in)
		if err != nil || got != c.want {
			t.Errorf("ReachOf(%v) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []any{map[string]any{"entity": "mailThread"}, map[string]any{"entity": "a b", "id": "1"}, 7, []any{3}} {
		if got, err := ReachOf(bad); err == nil {
			t.Errorf("ReachOf(%v) = %q, want an error", bad, got)
		}
	}
}
