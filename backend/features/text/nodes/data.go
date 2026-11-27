package textnodes

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/text"
)

// DataDiffID is the Data diff node (forge 22b).
const DataDiffID = "data.diff"

func contributeData(reg *extensions.Registry) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: DataDiffID, Source: source, Order: 475}, Title: "Data diff", Category: "Text",
		Description: "The changes between two JSON values as things, not text: [{op: added | removed | changed | moved, path, before, after}], where path is list[key].field. Keys names the lists whose items are matched by a field rather than by position, by the list's path with [] for list items: {\"sites\": \"id\", \"profiles[].units\": \"name\"}; a keyed item that changed place among the others is moved. Equal values give no changes, whatever order their maps' keys are in. For a configuration's versions, a record's before and after, or any structured document.",
		Example:     "Data diff of two infrastructure configuration versions' documents, with keys {sites: id, hostPools: id}",
		Tags:        []string{"diff", "compare", "json", "structured", "changes"},
		Config:      json.RawMessage(`{"type":"object","properties":{"keys":{"type":"object","title":"Keys","description":"List path to key field, e.g. {\"sites\": \"id\"}; the Keys pin wins"}}}`),
		Sample:      cognition.Values{"changes": []any{}, "changed": false},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "a", Name: "Old", Type: "json", Optional: true, Description: "The old value; null when unconnected"},
				{ID: "b", Name: "New", Type: "json", Optional: true, Description: "The new value; null when unconnected"},
				{ID: "keys", Name: "Keys", Type: "json", Optional: true, Description: "List path to key field, in place of its config's"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("changes", "Changes", "json", "The changes in order: [{op, path, before, after}]"),
				cognition.DataPin("changed", "Changed", "bool", "Whether the values differ"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct {
				Keys map[string]string `json:"keys"`
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if k, ok := in["keys"]; ok && k != nil {
				raw, _ := json.Marshal(k)
				var keys map[string]string
				if json.Unmarshal(raw, &keys) == nil {
					c.Keys = keys
				}
			}
			changes := text.DataDiff(in["a"], in["b"], c.Keys)
			out, err := plain(changes)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"changes": out, "changed": len(changes) > 0}, "", nil
		},
	})
}
