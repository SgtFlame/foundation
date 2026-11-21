package ops

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
)

// Item is one thing an op changes (forge 06b question 1): a prefab node by
// key path (its subtree with it), an entity field or index by id, a
// manifest list item by key, a graph node, edge or variable by id, or a
// scalar field by name. Kind "" is the whole target: an op that makes,
// replaces, deletes or restores it.
type Item struct {
	Target string `json:"target"` // <targetType>:<id>
	Kind   string `json:"kind,omitempty"`
	Key    string `json:"key,omitempty"`
}

func (i Item) String() string {
	if i.Kind == "" {
		return i.Target
	}
	return i.Target + " " + i.Kind + " " + i.Key
}

// Overlaps reports whether two items are the same thing, or one holds the
// other: the whole target holds every item, and a node its subtree.
func (i Item) Overlaps(o Item) bool {
	if i.Target != o.Target {
		return false
	}
	if i.Kind == "" || o.Kind == "" {
		return true
	}
	if i.Kind != o.Kind {
		return false
	}
	if i.Key == o.Key {
		return true
	}
	return i.Kind == "node" && (strings.HasPrefix(i.Key, o.Key+"/") || strings.HasPrefix(o.Key, i.Key+"/"))
}

// Items are what an entry changes, read from its arguments and from its
// inverse's (which name what it made: the key a node got, a new field's id).
func Items(e Entry) []Item {
	target := e.TargetType + ":" + e.Target
	var out []Item
	add := func(kind, key string) {
		it := Item{Target: target, Kind: kind, Key: key}
		if (kind == "" || key != "") && !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	itemsOf(e.Command, e.Args, add)
	for _, inv := range e.Inverse {
		if inv.Target == e.Target {
			itemsOf(inv.Command, inv.Args, add)
		}
	}
	return out
}

// whole are the commands that make, replace, delete or restore a target.
var whole = []string{"create", "import", "delete", "restore", "setManifest"}

func itemsOf(command string, raw json.RawMessage, add func(kind, key string)) {
	family, name, _ := strings.Cut(command, ".")
	if slices.Contains(whole, name) {
		add("", "")
		return
	}
	var a map[string]any
	_ = json.Unmarshal(raw, &a)
	str := func(k string) string { s, _ := a[k].(string); return s }
	strs := func(k string) []string {
		var out []string
		list, _ := a[k].([]any)
		for _, v := range list {
			switch v := v.(type) {
			case string:
				out = append(out, v)
			case map[string]any:
				if id, ok := v["id"].(string); ok {
					out = append(out, id)
				}
			}
		}
		return out
	}
	fields := func(kind string) {
		for k := range a {
			add(kind, k)
		}
	}
	switch family {
	case "prefab":
		switch name {
		case "setInfo":
			fields("info")
		case "setInterface":
			fields("interface")
		case "attachGraph", "detachGraph":
			add("graph", str("name"))
		case "setPreview":
			add("preview", "props")
		case "setVariants":
			add("variants", "variants")
		case "insertNode":
			if n, ok := a["node"].(map[string]any); ok {
				key, _ := n["key"].(string)
				add("node", strings.TrimPrefix(str("parentPath")+"/"+key, "/"))
			}
		default:
			add("node", str("path"))
			for _, p := range strs("paths") {
				add("node", p)
			}
		}
	case "bp":
		switch name {
		case "setInfo":
			fields("info")
		case "addVariable", "removeVariable":
			add("variable", str("id"))
		case "addNode", "setConfig":
			add("node", str("id"))
			add("node", str("node"))
		case "moveNodes":
			list, _ := a["moves"].([]any)
			for _, m := range list {
				if m, ok := m.(map[string]any); ok {
					node, _ := m["node"].(string)
					add("node", node)
				}
			}
		case "connect", "disconnect":
			add("edge", edgeKey(a["from"], a["to"]))
		case "remove", "paste":
			for _, id := range strs("nodes") {
				add("node", id)
			}
			list, _ := a["edges"].([]any)
			for _, x := range list {
				if x, ok := x.(map[string]any); ok {
					add("edge", edgeKey(x["from"], x["to"]))
				}
			}
		}
	case "entity":
		switch name {
		case "setInfo":
			fields("info")
		case "setAccess":
			add("access", "access")
		case "addField":
			add("field", str("id"))
		case "addIndex":
			add("index", str("id"))
		default:
			add("field", str("field"))
			add("index", str("index"))
		}
	case "app":
		switch {
		case name == "setInfo":
			fields("info")
		case name == "setNav":
			add("nav", "nav")
		case name == "setSettingsSchema":
			add("settings", "settings")
		case name == "setSeed":
			add("seed", "seed")
		case strings.HasSuffix(name, "Entity"):
			add("entity", str("entity"))
		default:
			for _, verb := range []string{"add", "update", "remove"} {
				item, ok := strings.CutPrefix(name, verb)
				if !ok || item == "" {
					continue
				}
				item = strings.ToLower(item[:1]) + item[1:]
				l, ok := manifests.ListOf(item)
				if !ok {
					continue
				}
				arg := l.Key
				if arg == "" {
					arg = l.Arg
				}
				add(item, str(arg))
				add(item, str("rename"))
			}
		}
	case "ui":
		if n, ok := a["node"].(map[string]any); ok {
			key, _ := n["key"].(string)
			add("node", key)
		}
		add("node", str("key"))
	}
}

func edgeKey(from, to any) string {
	pin := func(v any) string {
		m, _ := v.(map[string]any)
		return fmt.Sprintf("%v:%v", m["node"], m["pin"])
	}
	return pin(from) + ">" + pin(to)
}
