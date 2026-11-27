package diff

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// NodeID is the Structured diff node's type, MergeNodeID the Structural
// three-way merge's.
const (
	NodeID      = "apps.diff"
	MergeNodeID = "apps.merge3"
)

// cosmetic are the fields whose change alone never needs a model to
// describe: what a part is called and how it describes itself.
var cosmetic = []string{"description", "title", "name", "tags", "example", "help", "helpText", "label"}

// Trivial reports whether every change is cosmetic: a move, or a change
// to a part's or item's name, title or description.
func Trivial(changes []Change) bool {
	for _, c := range changes {
		if c.Kind == Moved {
			continue
		}
		field := c.Path[strings.LastIndexAny(c.Path, ".]")+1:]
		if c.Kind != Changed && c.Kind != Added && c.Kind != Removed || !slices.Contains(cosmetic, field) {
			return false
		}
	}
	return true
}

// Lines are changes as lines for a prompt or a person, values cut short.
func Lines(changes []Change) string {
	var b strings.Builder
	val := func(v any) string {
		raw, _ := json.Marshal(v)
		s := string(raw)
		if len(s) > 300 {
			s = s[:300] + "…"
		}
		return s
	}
	for _, c := range changes {
		fmt.Fprintf(&b, "- %s %s", c.Kind, c.Path)
		if c.Before != nil {
			fmt.Fprintf(&b, " before %s", val(c.Before))
		}
		if c.After != nil {
			fmt.Fprintf(&b, " after %s", val(c.After))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Contribute registers the Structured diff node (forge 19d) and the
// Structural three-way merge (forge 19e).
func Contribute(reg *extensions.Registry) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: MergeNodeID, Source: "apps", Order: 481}, Title: "Structural three-way merge", Category: "Apps",
		Description: "Merges two sides' changes to one app part since their base (forge 19e): Base, Ours (the side merged into) and Theirs (the side merged from) are the part's definitions or drafts, empty for none. Changes at different paths merge, changes at one path that agree merge, a graph node's position never conflicts (ours wins), and lists merge by their keys (fields by id, nodes by id, edges by their ends, prefab children by key, pages by route, roles by id). Disagreements, a value set two ways or an item removed on one side and changed on the other, are Conflicts [{path, base, ours, theirs, absent}], which take ours in Merged until Resolutions ({path: {take: ours | theirs | base | value, value}}) settle them.",
		Example:     "Structural three-way merge of a prefab changed on two branches, the conflicts sent to app-mergePart",
		Tags:        []string{"merge", "three-way", "conflict", "app", "branch", "structured"},
		Sample:      cognition.Values{"merged": map[string]any{}, "conflicts": []any{}, "clean": true},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "type", Name: "Type", Type: "string", Description: "The part's type: app, prefab, graph or entity"},
				{ID: "id", Name: "Id", Type: "string", Optional: true, Description: "The part's id, for the conflicts' paths"},
				{ID: "base", Name: "Base", Type: "json", Optional: true, Description: "The part at the common ancestor; empty when neither had it"},
				{ID: "ours", Name: "Ours", Type: "json", Optional: true, Description: "The part on the side merged into; empty when it removed it"},
				{ID: "theirs", Name: "Theirs", Type: "json", Optional: true, Description: "The part on the side merged from; empty when it removed it"},
				{ID: "resolutions", Name: "Resolutions", Type: "json", Optional: true, Description: "How conflicts are settled: {path: {take, value}}"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("merged", "Merged", "json", "The merged definition, ours where a conflict is open; empty when the part is gone"),
				cognition.DataPin("conflicts", "Conflicts", "json", "The open conflicts: [{path, base, ours, theirs, absent}]"),
				cognition.DataPin("clean", "Clean", "bool", "Whether no conflict is open"),
			},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			typ, _ := cognition.Get[string](in, "type")
			id, _ := cognition.Get[string](in, "id")
			if _, ok := Inner[typ]; !ok {
				return nil, "", fmt.Errorf("type is app, prefab, graph or entity, not %q", typ)
			}
			def := func(pin string) map[string]any {
				if in[pin] == nil {
					return nil
				}
				raw, _ := json.Marshal(in[pin])
				return definition(typ, raw)
			}
			var res map[string]Resolution
			if in["resolutions"] != nil {
				raw, _ := json.Marshal(in["resolutions"])
				_ = json.Unmarshal(raw, &res)
			}
			merged, conflicts := Merge3(typ, id, def("base"), def("ours"), def("theirs"), res)
			out := []any{}
			raw, _ := json.Marshal(conflicts)
			_ = json.Unmarshal(raw, &out)
			var m any
			if merged != nil {
				m = merged
			}
			return cognition.Values{"merged": m, "conflicts": out, "clean": len(conflicts) == 0}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodeID, Source: "apps", Order: 480}, Title: "Structured diff", Category: "Apps",
		Description: "What changed in an app's parts, as data (forge 19d): given two states of one part (Type, Id, Before, After: a draft or the definition itself, empty for none), or a commit's structured diff (Parts: [{type, id, changes}], as app.commit keeps it), gives every change {path, kind: added | removed | changed | moved, before, after} with a path a person can follow (entity:refund.fields.limit.type, prefab:orderRow.tree.root/actions, app:shop.pages[/orders].title). Entity types diff their fields by stable id (a rename is a change of name), graphs their nodes by id (a node only repositioned is moved) and edges by their ends, prefabs their component tree by key path (a subtree under a new parent is one move), manifests their pages, roles, commands and other keyed lists by key. Trivial is true when every change is only a move or a name, title or description, for a semantic diff written without a model; Lines are the changes as text for a prompt.",
		Example:     "Structured diff of a commit's parts, for app-semanticDiff to describe them or, when trivial, to summarize them without a model",
		Tags:        []string{"diff", "compare", "app", "commit", "changes", "structured"},
		Sample:      cognition.Values{"changes": []any{}, "trivial": true, "lines": "", "count": 0},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "parts", Name: "Parts", Type: "json", Optional: true, Description: "A commit's structured diff, [{type, id, changes}]; when given, the other inputs are not read"},
				{ID: "type", Name: "Type", Type: "string", Optional: true, Description: "The part's type: app, prefab, graph or entity"},
				{ID: "id", Name: "Id", Type: "string", Optional: true, Description: "The part's id"},
				{ID: "before", Name: "Before", Type: "json", Optional: true, Description: "The part before: its draft or definition; empty when it is new"},
				{ID: "after", Name: "After", Type: "json", Optional: true, Description: "The part after; empty when it is removed"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("changes", "Changes", "json", "Every change in order: [{path, kind, before, after}]"),
				cognition.DataPin("trivial", "Trivial", "bool", "Whether every change is only a move or a name, title or description"),
				cognition.DataPin("lines", "Lines", "string", "The changes as one line each, values cut short, for a prompt"),
				cognition.DataPin("count", "Count", "number", "How many changes"),
			},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var changes []Change
			if p, ok := in["parts"]; ok && p != nil {
				raw, _ := json.Marshal(p)
				var parts []struct {
					Changes []Change `json:"changes"`
				}
				if err := json.Unmarshal(raw, &parts); err != nil {
					return nil, "", fmt.Errorf("parts are [{type, id, changes}]: %v", err)
				}
				for _, pd := range parts {
					changes = append(changes, pd.Changes...)
				}
			} else {
				typ, _ := cognition.Get[string](in, "type")
				id, _ := cognition.Get[string](in, "id")
				if _, ok := Inner[typ]; !ok {
					return nil, "", fmt.Errorf("type is app, prefab, graph or entity, not %q", typ)
				}
				state := func(pin string) json.RawMessage {
					if in[pin] == nil {
						return nil
					}
					raw, _ := json.Marshal(in[pin])
					return raw
				}
				changes = Part(typ, id, state("before"), state("after"))
			}
			out := []any{}
			raw, _ := json.Marshal(changes)
			_ = json.Unmarshal(raw, &out)
			if out == nil {
				out = []any{}
			}
			return cognition.Values{"changes": out, "trivial": Trivial(changes), "lines": Lines(changes), "count": len(changes)}, "", nil
		},
	})
}
