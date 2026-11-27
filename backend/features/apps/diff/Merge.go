package diff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Conflict is one place where both sides changed a part differently since
// their base (forge 19e): the base's value and each side's, absent ones
// left out. Ours is the side merged into (the target), theirs the side
// merged from (the source).
type Conflict struct {
	Path   string `json:"path"`
	Base   any    `json:"base,omitempty"`
	Ours   any    `json:"ours,omitempty"`
	Theirs any    `json:"theirs,omitempty"`
	// Absent names the sides that have no value there: base, ours, theirs
	Absent []string `json:"absent,omitempty"`
}

// Resolution is what a conflict's path takes: ours, theirs, base, or a
// value given.
type Resolution struct {
	Take  string `json:"take"` // ours, theirs, base or value
	Value any    `json:"value,omitempty"`
}

// missing marks a value a side does not have.
type missingT struct{}

var missing = missingT{}

// Merge3 is the structural three-way merge of a part's definitions (forge
// 19e): base, ours and theirs (nil for no part). Changes at different
// paths merge; changes at one path that agree merge; a graph node's
// position never conflicts (ours wins); other disagreements, a value set
// two ways or an item removed on one side and changed on the other, are
// conflicts, which take ours in merged until resolved. resolved gives the
// value a conflict's path takes. merged is nil when the part is gone.
func Merge3(typ, id string, base, ours, theirs map[string]any, resolved map[string]Resolution) (merged map[string]any, conflicts []Conflict) {
	m := &merger{resolved: resolved, conflicts: []Conflict{}}
	keys := genericKeys
	switch typ {
	case "app":
		keys = func(path string) func(any) string {
			if k := manifestKeys(path); k != nil {
				return k
			}
			return genericKeys(path)
		}
	case "graph":
		keys = graphKeys
	case "prefab":
		keys = prefabKeys
	}
	m.keys = keys
	v := m.merge(typ+":"+id, orMissing(base), orMissing(ours), orMissing(theirs))
	out, _ := v.(map[string]any)
	return out, m.conflicts
}

func orMissing(v map[string]any) any {
	if v == nil {
		return missing
	}
	return v
}

type merger struct {
	keys      keyOf
	resolved  map[string]Resolution
	conflicts []Conflict
}

func eq(a, b any) bool {
	if a == missing || b == missing {
		return a == b
	}
	if empty(a) && empty(b) {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func (m *merger) merge(path string, b, o, t any) any {
	switch {
	case eq(o, t):
		return o
	case eq(b, o):
		return t
	case eq(b, t):
		return o
	}
	if strings.HasSuffix(path, ".position") {
		return o // layout never conflicts: the target's position wins
	}
	bm, bok := asMap(b)
	om, ook := asMap(o)
	tm, tok := asMap(t)
	if b == missing && ook && tok {
		bm, bok = map[string]any{}, true // both added it: merge what each holds
	}
	if bok && ook && tok {
		out := map[string]any{}
		for _, k := range unionOf(bm, om, tm) {
			v := m.merge(path+"."+k, get(bm, k), get(om, k), get(tm, k))
			if v != missing {
				out[k] = v
			}
		}
		return out
	}
	bl, bok := asList(b)
	ol, ook := asList(o)
	tl, tok := asList(t)
	if b == missing && ook && tok {
		bl, bok = []any{}, true
	}
	if bok && ook && tok {
		if key := m.keys(path); key != nil && keyed(bl, key) && keyed(ol, key) && keyed(tl, key) {
			return m.list(path, bl, ol, tl, key)
		}
	}
	return m.conflict(path, b, o, t)
}

// list merges a keyed list: ours' order, theirs' new items after them.
func (m *merger) list(path string, b, o, t []any, key func(any) string) any {
	bi, oi, ti := index(b, key), index(o, key), index(t, key)
	at := func(l []any, idx map[string]int, k string) any {
		if i, ok := idx[k]; ok {
			return l[i]
		}
		return missing
	}
	out := []any{}
	seen := map[string]bool{}
	var order []string
	for _, it := range o {
		order = append(order, key(it))
	}
	for _, it := range t {
		if _, ok := oi[key(it)]; !ok {
			order = append(order, key(it))
		}
	}
	for _, it := range b {
		if k := key(it); !slices.Contains(order, k) {
			order = append(order, k) // removed on both sides: merges to nothing
		}
	}
	for _, k := range order {
		if seen[k] {
			continue
		}
		seen[k] = true
		v := m.merge(fmt.Sprintf("%s[%s]", path, k), at(b, bi, k), at(o, oi, k), at(t, ti, k))
		if v != missing {
			out = append(out, v)
		}
	}
	return out
}

func (m *merger) conflict(path string, b, o, t any) any {
	if r, ok := m.resolved[path]; ok {
		switch r.Take {
		case "theirs":
			return t
		case "base":
			return b
		case "value":
			if r.Value == nil {
				return missing
			}
			return r.Value
		}
		return o
	}
	c := Conflict{Path: path}
	for _, side := range []struct {
		name string
		v    any
		set  *any
	}{{"base", b, &c.Base}, {"ours", o, &c.Ours}, {"theirs", t, &c.Theirs}} {
		if side.v == missing {
			c.Absent = append(c.Absent, side.name)
		} else {
			*side.set = side.v
		}
	}
	m.conflicts = append(m.conflicts, c)
	return o
}

func asMap(v any) (map[string]any, bool) {
	if v == missing {
		return nil, false
	}
	x, ok := v.(map[string]any)
	return x, ok
}

func asList(v any) ([]any, bool) {
	if v == missing {
		return nil, false
	}
	x, ok := v.([]any)
	return x, ok
}

func get(m map[string]any, k string) any {
	if v, ok := m[k]; ok {
		return v
	}
	return missing
}

func unionOf(ms ...map[string]any) []string {
	keys := map[string]any{}
	for _, m := range ms {
		for k := range m {
			keys[k] = true
		}
	}
	return union(keys, nil)
}

// genericKeys keys lists of objects that all have an id or a key.
func genericKeys(path string) func(any) string {
	return by("id", "key")
}

func graphKeys(path string) func(any) string {
	if strings.HasSuffix(path, ".edges") {
		return func(e any) string {
			m, _ := e.(map[string]any)
			end := func(k string) string {
				x, _ := m[k].(map[string]any)
				return fmt.Sprintf("%v.%v", x["node"], x["pin"])
			}
			return end("from") + "->" + end("to")
		}
	}
	return by("id", "name")
}

func prefabKeys(path string) func(any) string {
	return by("key", "id", "name")
}

// Definition is a part's definition in a draft state (nil for none or
// a removed part), as plain JSON values, for Merge3.
func Definition(typ string, raw json.RawMessage) map[string]any { return definition(typ, raw) }

// WithDefinition is a draft state with its definition replaced by def.
func WithDefinition(typ string, state json.RawMessage, def map[string]any) (json.RawMessage, error) {
	var v map[string]any
	if len(state) > 0 && json.Unmarshal(state, &v) == nil && v[Inner[typ]] != nil {
		v[Inner[typ]] = def
		return json.Marshal(v)
	}
	return json.Marshal(def)
}
