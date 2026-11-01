package cognitionnodes

import (
	"slices"
	"testing"
)

func TestIdentifiersAreTheExpressionsInputs(t *testing.T) {
	for src, want := range map[string][]string{
		"count + 1":                           {"count"},
		`"Hello " + name`:                     {"name"},
		"source != target":                    {"source", "target"},
		"len(items) > max ? max : len(items)": {"items", "max"},
		"let x = a * 2; x + b":                {"a", "b"},
		"state.value ?? 0":                    {"state"},
	} {
		got, err := Identifiers(src)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Identifiers(%q) = %v, %v; want %v", src, got, err, want)
		}
	}
	v, err := Eval("(args.by ?? 1) * 2", map[string]any{"args": map[string]any{}})
	if err != nil || v != 2 {
		t.Errorf("Eval = %v, %v", v, err)
	}
	if v, err := Eval("count + 1", map[string]any{"count": 41}); err != nil || v != 42 {
		t.Errorf("an input named count = %v, %v", v, err)
	}
	if got := groupThousands("-1234567.50"); got != "-1,234,567.50" {
		t.Errorf("groupThousands = %s", got)
	}
}
