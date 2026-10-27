package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Values are decoded JSON: map[string]any, []any, float64, string, bool.

// Decode decodes a JSON object; empty input is an empty object.
func Decode(raw []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// Clone deep-copies a value.
func Clone(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = Clone(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = Clone(x)
		}
		return out
	}
	return v
}

// Merge applies patch to target as a JSON merge patch (RFC 7386) and returns
// the result: objects merge key by key, null removes a key, and anything
// else, arrays included, replaces. Neither argument is modified.
func Merge(target, patch map[string]any) map[string]any {
	out := Clone(target).(map[string]any)
	for k, p := range patch {
		if p == nil {
			delete(out, k)
			continue
		}
		pm, pIsObj := p.(map[string]any)
		tm, tIsObj := out[k].(map[string]any)
		switch {
		case pIsObj && tIsObj:
			out[k] = Merge(tm, pm)
		case pIsObj:
			out[k] = Merge(map[string]any{}, pm)
		default:
			out[k] = Clone(p)
		}
	}
	return out
}

// Get is the value at a dotted path, array items by index.
func Get(v any, path string) (any, bool) {
	for _, seg := range splitPath(path) {
		switch cur := v.(type) {
		case map[string]any:
			x, ok := cur[seg]
			if !ok {
				return nil, false
			}
			v = x
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(cur) {
				return nil, false
			}
			v = cur[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// Set puts value at a dotted path of objects, making the objects on the way.
func Set(obj map[string]any, path string, value any) {
	segs := splitPath(path)
	for _, seg := range segs[:len(segs)-1] {
		next, ok := obj[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			obj[seg] = next
		}
		obj = next
	}
	obj[segs[len(segs)-1]] = value
}

// Delete removes the value at a dotted path of objects, and the objects left empty.
func Delete(obj map[string]any, path string) {
	segs := splitPath(path)
	if len(segs) == 0 {
		return
	}
	if len(segs) == 1 {
		delete(obj, segs[0])
		return
	}
	child, ok := obj[segs[0]].(map[string]any)
	if !ok {
		return
	}
	Delete(child, strings.Join(segs[1:], "."))
	if len(child) == 0 {
		delete(obj, segs[0])
	}
}
