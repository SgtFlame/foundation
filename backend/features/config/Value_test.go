package config

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	target := map[string]any{"a": map[string]any{"b": 1.0, "c": 2.0}, "list": []any{1.0}, "gone": true}
	patch := map[string]any{"a": map[string]any{"c": 3.0}, "list": []any{2.0, 3.0}, "gone": nil, "new": map[string]any{"x": "y"}}
	got := Merge(target, patch)
	want := map[string]any{"a": map[string]any{"b": 1.0, "c": 3.0}, "list": []any{2.0, 3.0}, "new": map[string]any{"x": "y"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Merge = %v", got)
	}
	if target["gone"] != true || target["a"].(map[string]any)["c"] != 2.0 {
		t.Errorf("Merge modified its target: %v", target)
	}

	Delete(got, "new.x")
	if _, ok := got["new"]; ok {
		t.Errorf("Delete left an empty object: %v", got)
	}
}
