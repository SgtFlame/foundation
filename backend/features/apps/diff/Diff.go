// Package diff is the structured diff of an app's parts (forge 19d,
// documentation/Apps.md › Built in forge 19d): what changed between two
// states of a part, as data a person and a model can follow. One function
// per part kind, behind Part: entity types diff their fields by stable id,
// graphs their nodes by id (a node only repositioned is moved) and edges
// by their ends, prefabs their template tree by key path (a subtree under
// a new parent is one move), manifests their keyed lists by key. The same
// changes serve the Structured diff node (apps.diff) and 19e's merge.
package diff

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// Kinds of change.
const (
	Added   = "added"
	Removed = "removed"
	Changed = "changed"
	Moved   = "moved"
)

// Change is one difference: a path a person can follow
// (entity:refund.fields.limit.type, prefab:orderRow.tree.root/actions,
// app:shop.pages[/orders].title), its kind, and the values either side.
type Change struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

// Inner is the key of a part type's definition inside its draft's state.
var Inner = map[string]string{"prefab": "prefab", "graph": "graph", "entity": "entity", "app": "manifest"}

// Part is the structured diff of one part of type typ (app, prefab, graph,
// entity) between two of its draft states, before and after, as the part
// projections hold them; nil (or the branch tombstone) is no part. A
// state may also be the definition itself, without its draft around it.
func Part(typ, id string, before, after json.RawMessage) []Change {
	a, b := definition(typ, before), definition(typ, after)
	root := typ + ":" + id
	switch {
	case a == nil && b == nil:
		return []Change{}
	case a == nil:
		return []Change{{Path: root, Kind: Added, After: summary(b)}}
	case b == nil:
		return []Change{{Path: root, Kind: Removed, Before: summary(a)}}
	}
	d := &differ{out: []Change{}}
	switch typ {
	case "prefab":
		d.prefab(root, a, b)
	case "graph":
		d.graph(root, a, b)
	case "app":
		d.walk(root, a, b, manifestKeys)
	default:
		d.walk(root, a, b, nil)
	}
	return d.out
}

// Definitions is Part given the definitions themselves: a manifest, a
// prefab, a graph or an entity type.
func Definitions(typ, id string, before, after any) []Change {
	raw := func(v any) json.RawMessage {
		if v == nil {
			return nil
		}
		b, _ := json.Marshal(map[string]any{Inner[typ]: v})
		return b
	}
	return Part(typ, id, raw(before), raw(after))
}

// definition is a draft state's definition as plain JSON values, nil for
// none.
func definition(typ string, raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil || v["$removed"] == true {
		return nil
	}
	if inner, ok := v[Inner[typ]].(map[string]any); ok {
		if v["deleted"] == true {
			return nil // a deleted prefab or app is gone, as a removed graph is
		}
		return inner
	}
	return v
}

// summary is what an added or removed part's change carries: its name.
func summary(v map[string]any) any {
	for _, k := range []string{"name", "title"} {
		if s, ok := v[k].(string); ok && s != "" {
			return map[string]any{k: s}
		}
	}
	return nil
}

type differ struct{ out []Change }

func (d *differ) add(c Change) { d.out = append(d.out, c) }

// keyOf names the key of a list's items at a path ("" for a list compared
// whole).
type keyOf func(path string) func(item any) string

// walk diffs two JSON values: objects per key, keyed lists per item,
// other values whole.
func (d *differ) walk(path string, a, b any, keys keyOf) {
	if reflect.DeepEqual(a, b) {
		return
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		for _, k := range union(am, bm) {
			p := path + "." + k
			av, ain := am[k]
			bv, bin := bm[k]
			switch {
			case !ain && empty(bv), !bin && empty(av):
			case !ain:
				d.add(Change{Path: p, Kind: Added, After: bv})
			case !bin:
				d.add(Change{Path: p, Kind: Removed, Before: av})
			default:
				d.walk(p, av, bv, keys)
			}
		}
		return
	}
	al, aok := a.([]any)
	bl, bok := b.([]any)
	if aok && bok && keys != nil {
		if key := keys(path); key != nil && keyed(al, key) && keyed(bl, key) {
			d.list(path, al, bl, key, keys)
			return
		}
	}
	d.add(Change{Path: path, Kind: Changed, Before: a, After: b})
}

// list diffs a keyed list: items added and removed by key, an item whose
// place among those both sides keep changed is moved, and each kept item
// is diffed in turn.
func (d *differ) list(path string, a, b []any, key func(any) string, keys keyOf) {
	ai, bi := index(a, key), index(b, key)
	var kept []string
	for _, it := range a {
		if _, ok := bi[key(it)]; ok {
			kept = append(kept, key(it))
		}
	}
	var keptB []string
	for _, it := range b {
		if _, ok := ai[key(it)]; ok {
			keptB = append(keptB, key(it))
		}
	}
	for i, it := range a {
		k := key(it)
		if _, ok := bi[k]; !ok {
			d.add(Change{Path: fmt.Sprintf("%s[%s]", path, k), Kind: Removed, Before: a[i]})
		}
	}
	for _, it := range b {
		k := key(it)
		p := fmt.Sprintf("%s[%s]", path, k)
		j, ok := ai[k]
		if !ok {
			d.add(Change{Path: p, Kind: Added, After: it})
			continue
		}
		if slices.Index(kept, k) != slices.Index(keptB, k) {
			d.add(Change{Path: p, Kind: Moved, Before: slices.Index(kept, k), After: slices.Index(keptB, k)})
		}
		d.walk(p, a[j], it, keys)
	}
}

// manifestKeys are a manifest's keyed lists (manifests.Lists) and its nav.
func manifestKeys(path string) func(any) string {
	field := path[strings.LastIndex(path, ".")+1:]
	switch field {
	case "pages", "nav":
		return by("route", "id", "title")
	case "editorKinds":
		return by("kind")
	case "commands":
		return by("type")
	case "queries", "tools", "connectors", "projectors":
		return by("name")
	case "ephemeral":
		return by("topic")
	case "roles", "triggers", "items", "rules":
		return by("id", "name")
	case "entities":
		return func(it any) string { s, _ := it.(string); return s }
	}
	return nil
}

// by keys items by the first of fields they hold as text.
func by(fields ...string) func(any) string {
	return func(it any) string {
		m, _ := it.(map[string]any)
		for _, f := range fields {
			if s, ok := m[f].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
}

// graph diffs a graph: its nodes by id (a change of position alone is a
// move), its edges by their ends, its variables by id, the rest by key.
func (d *differ) graph(root string, a, b map[string]any) {
	rest := func(m map[string]any) map[string]any {
		out := maps.Clone(m)
		delete(out, "nodes")
		delete(out, "edges")
		delete(out, "id")
		return out
	}
	d.walk(root, rest(a), rest(b), func(path string) func(any) string {
		if strings.HasSuffix(path, ".variables") {
			return by("id", "name")
		}
		return nil
	})
	an, bn := list(a["nodes"]), list(b["nodes"])
	id := by("id")
	ai, bi := index(an, id), index(bn, id)
	for _, n := range an {
		if _, ok := bi[id(n)]; !ok {
			d.add(Change{Path: root + ".nodes." + id(n), Kind: Removed, Before: brief(n)})
		}
	}
	for _, n := range bn {
		p := root + ".nodes." + id(n)
		j, ok := ai[id(n)]
		if !ok {
			d.add(Change{Path: p, Kind: Added, After: brief(n)})
			continue
		}
		old, _ := an[j].(map[string]any)
		cur, _ := n.(map[string]any)
		if !reflect.DeepEqual(old["position"], cur["position"]) {
			d.add(Change{Path: p, Kind: Moved, Before: old["position"], After: cur["position"]})
		}
		strip := func(m map[string]any) map[string]any {
			out := maps.Clone(m)
			delete(out, "position")
			return out
		}
		d.walk(p, strip(old), strip(cur), nil)
	}
	edge := func(e any) string {
		m, _ := e.(map[string]any)
		end := func(k string) string {
			x, _ := m[k].(map[string]any)
			return fmt.Sprintf("%v.%v", x["node"], x["pin"])
		}
		return end("from") + "->" + end("to")
	}
	ae, be := list(a["edges"]), list(b["edges"])
	aei, bei := index(ae, edge), index(be, edge)
	for _, e := range ae {
		if _, ok := bei[edge(e)]; !ok {
			d.add(Change{Path: root + ".edges[" + edge(e) + "]", Kind: Removed})
		}
	}
	for _, e := range be {
		if _, ok := aei[edge(e)]; !ok {
			d.add(Change{Path: root + ".edges[" + edge(e) + "]", Kind: Added})
		}
	}
}

// brief is a node as an added or removed change carries it.
func brief(n any) any {
	m, _ := n.(map[string]any)
	out := map[string]any{"type": m["type"]}
	if t, ok := m["title"]; ok {
		out["title"] = t
	}
	return out
}

// prefab diffs a prefab: its template as a tree of key paths (a node is
// added, removed, moved to another parent or place, or changed in its own
// fields), the rest by key.
func (d *differ) prefab(root string, a, b map[string]any) {
	rest := func(m map[string]any) map[string]any {
		out := maps.Clone(m)
		delete(out, "template")
		delete(out, "version")
		return out
	}
	d.walk(root, rest(a), rest(b), nil)
	at, bt := flatten(a["template"]), flatten(b["template"])
	// A node is known by its key, which is unique in a template; its path
	// says where it is. A node removed or added with its parent is part of the parent's change
	for _, k := range slices.Sorted(maps.Keys(at)) {
		if _, ok := bt[k]; !ok && !removedWithParent(at, bt, k) {
			d.add(Change{Path: root + ".tree." + at[k].path, Kind: Removed, Before: at[k].own["type"]})
		}
	}
	for _, k := range sortedByPath(bt) {
		n := bt[k]
		p := root + ".tree." + n.path
		o, ok := at[k]
		if !ok {
			if !addedWithParent(at, bt, k) {
				d.add(Change{Path: p, Kind: Added, After: n.own["type"]})
			}
			continue
		}
		if o.parentKey != n.parentKey || o.slot != n.slot || rank(at, bt, k) != rank(bt, at, k) {
			// One move for a subtree: its children keep their parent
			d.add(Change{Path: p, Kind: Moved, Before: o.path, After: n.path})
		}
		d.walk(p, o.own, n.own, nil)
	}
}

// rank is a node's place among its siblings in t that are siblings in
// other too, so that inserting or removing a sibling moves nothing.
func rank(t, other map[string]treeNode, k string) int {
	me := t[k]
	r := 0
	for sk, s := range t {
		o, ok := other[sk]
		if sk != k && s.parentKey == me.parentKey && s.slot == me.slot && s.place < me.place &&
			ok && o.parentKey == me.parentKey && o.slot == me.slot {
			r++
		}
	}
	return r
}

func removedWithParent(at, bt map[string]treeNode, k string) bool {
	p := at[k].parentKey
	_, kept := bt[p]
	return p != "" && !kept
}

func addedWithParent(at, bt map[string]treeNode, k string) bool {
	p := bt[k].parentKey
	_, had := at[p]
	return p != "" && !had
}

type treeNode struct {
	path, parentKey, slot string
	place                 int
	own                   map[string]any // its fields but children and slots
}

// flatten is a template's nodes by key.
func flatten(t any) map[string]treeNode {
	out := map[string]treeNode{}
	var visit func(n any, parent treeNode, parentKey, slot string, place int)
	visit = func(n any, parent treeNode, parentKey, slot string, place int) {
		m, ok := n.(map[string]any)
		if !ok {
			return
		}
		key, _ := m["key"].(string)
		path := key
		if parent.path != "" {
			path = parent.path + "/" + key
		}
		own := maps.Clone(m)
		delete(own, "children")
		delete(own, "slots")
		delete(own, "src")
		me := treeNode{path: path, parentKey: parentKey, slot: slot, place: place, own: own}
		out[key] = me
		for i, c := range list(m["children"]) {
			visit(c, me, key, "", i)
		}
		slots, _ := m["slots"].(map[string]any)
		for _, s := range slices.Sorted(maps.Keys(slots)) {
			for i, c := range list(slots[s]) {
				visit(c, me, key, s, i)
			}
		}
	}
	visit(t, treeNode{}, "", "", 0)
	return out
}

func sortedByPath(t map[string]treeNode) []string {
	ks := slices.Collect(maps.Keys(t))
	slices.SortFunc(ks, func(a, b string) int { return strings.Compare(t[a].path, t[b].path) })
	return ks
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func index(l []any, key func(any) string) map[string]int {
	out := map[string]int{}
	for i, it := range l {
		out[key(it)] = i
	}
	return out
}

// keyed reports whether every item has a key, each once.
func keyed(l []any, key func(any) string) bool {
	seen := map[string]bool{}
	for _, it := range l {
		k := key(it)
		if k == "" || seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}

func union(a, b map[string]any) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return slices.Sorted(maps.Keys(keys))
}

// empty values are no value: an omitted field and an empty one are the same.
func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
