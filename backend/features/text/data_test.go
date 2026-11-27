package text

import (
	"encoding/json"
	"testing"
)

func TestDataDiff(t *testing.T) {
	a := map[string]any{
		"sites":    []any{map[string]any{"id": "west", "region": "r1"}, map[string]any{"id": "east", "region": "r2"}, map[string]any{"id": "lab"}},
		"settings": map[string]any{"onDrift": "report"},
		"tags":     []any{"a", "b"},
	}
	b := map[string]any{
		"sites":    []any{map[string]any{"id": "east", "region": "r2"}, map[string]any{"id": "west", "region": "r9"}, map[string]any{"id": "north"}},
		"settings": map[string]any{"onDrift": "correct"},
		"tags":     []any{"a", "b", "c"},
	}
	got := DataDiff(a, b, map[string]string{"sites": "id"})
	raw, _ := json.Marshal(got)
	want := `[{"op":"changed","path":"settings.onDrift","before":"report","after":"correct"},` +
		`{"op":"changed","path":"sites[west].region","before":"r1","after":"r9"},` +
		`{"op":"removed","path":"sites[lab]","before":{"id":"lab"}},` +
		`{"op":"moved","path":"sites[west]","before":0,"after":1},` +
		`{"op":"moved","path":"sites[east]","before":1,"after":0},` +
		`{"op":"added","path":"sites[north]","after":{"id":"north"}},` +
		`{"op":"added","path":"tags[2]","after":"c"}]`
	if string(raw) != want {
		t.Fatalf("got  %s\nwant %s", raw, want)
	}
	if len(DataDiff(a, a, nil)) != 0 {
		t.Fatal("equal values differ")
	}
}
