package ui

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
)

// Patch is one change to the browser's tree, addressed by key path (the
// keys below the root; the empty path is the root).
type Patch struct {
	Op    string         `json:"op"` // replace, insert, remove, move, props
	Path  []string       `json:"path"`
	Node  *Node          `json:"node,omitempty"`  // replace, insert
	Index int            `json:"index,omitempty"` // insert, move: the position among the children
	Key   string         `json:"key,omitempty"`   // move: the child moved
	Props map[string]any `json:"props,omitempty"` // props: values to set; null removes one
}

// Diff is the patches that turn old into new, matching children by key.
// The browser applies them in order.
func Diff(path []string, old, new *Node) []Patch {
	if old == nil || old.Type != new.Type || old.Key != new.Key || !equalJSON(old.Bind, new.Bind) || !equalJSON(old.On, new.On) || !equalJSON(old.Slots, new.Slots) {
		return []Patch{{Op: "replace", Path: path, Node: new}}
	}
	var patches []Patch
	if changed := propsDiff(old.Props, new.Props); changed != nil {
		patches = append(patches, Patch{Op: "props", Path: path, Props: changed})
	}
	// Removals first, then each new child in order: inserted, moved into
	// place, then diffed
	keys := map[string]*Node{}
	for _, c := range new.Children {
		keys[c.Key] = c
	}
	var current []string
	olds := map[string]*Node{}
	for _, c := range old.Children {
		if keys[c.Key] == nil {
			patches = append(patches, Patch{Op: "remove", Path: child(path, c.Key)})
			continue
		}
		current = append(current, c.Key)
		olds[c.Key] = c
	}
	for i, c := range new.Children {
		o := olds[c.Key]
		if o == nil {
			patches = append(patches, Patch{Op: "insert", Path: path, Index: i, Node: c})
			current = insertAt(current, i, c.Key)
			continue
		}
		if at := indexOf(current, c.Key); at != i {
			patches = append(patches, Patch{Op: "move", Path: path, Key: c.Key, Index: i})
			current = insertAt(removeAt(current, at), i, c.Key)
		}
		patches = append(patches, Diff(child(path, c.Key), o, c)...)
	}
	return patches
}

func propsDiff(old, new map[string]any) map[string]any {
	var out map[string]any
	set := func(k string, v any) {
		if out == nil {
			out = map[string]any{}
		}
		out[k] = v
	}
	for k, v := range new {
		if ov, ok := old[k]; !ok || !equalJSON(ov, v) {
			set(k, v)
		}
	}
	for k := range old {
		if _, ok := new[k]; !ok {
			set(k, nil)
		}
	}
	return out
}

func child(path []string, key string) []string {
	return append(append([]string(nil), path...), key)
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func insertAt(list []string, i int, s string) []string {
	list = append(list, "")
	copy(list[i+1:], list[i:])
	list[i] = s
	return list
}

func removeAt(list []string, i int) []string {
	return append(list[:i], list[i+1:]...)
}

// getPath reads a dotted path in decoded JSON; array items by index.
func getPath(v any, path string) any {
	if path == "" {
		return v
	}
	for _, part := range strings.Split(path, ".") {
		switch cur := v.(type) {
		case map[string]any:
			v = cur[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(cur) {
				return nil
			}
			v = cur[i]
		default:
			return nil
		}
	}
	return v
}

// setPath sets a dotted path in a map, making the maps on the way.
func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := m[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[part] = next
		}
		m = next
	}
	if v == nil {
		delete(m, parts[len(parts)-1])
		return
	}
	m[parts[len(parts)-1]] = v
}

// related reports whether a dependency and a changed source overlap: the
// same, or one a dotted prefix of the other.
func related(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(b, a) && (b[len(a)] == '.')
}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
