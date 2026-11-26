// Package kbnodes is the collaboration rooms' Cognition node (forge 18c):
// Edit text together changes a record's markdown field through its room,
// so everyone who has it open sees the change as they type.
package kbnodes

import (
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

const source = "kb"

// EditID is Edit text together's node type id.
const EditID = "collab.edit"

// recordKind is the records' document kind (records/data.DocumentKind),
// named here so this package need not import records.
const recordKind = "record"

// Contribute registers the node; collab gives the service once it exists.
func Contribute(reg *extensions.Registry, collab func() collabservice.Service) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: EditID, Source: source, Order: 475}, Title: "Edit text together", Category: "Text",
		Description: "Changes a record's markdown field as the run's principal, who must be able to change the record: while someone has the field open in doc.markdown, the room's writer applies the edits to the shared text, so everyone sees them at once and their typing is kept; otherwise the field is saved with record.update. Each edit is {find, replace}, find occurring exactly once, or {start, end, replace}, a range of the field's saved text in characters (end exclusive, as Anchor resolve gives it), with find the text expected there. With Each, the edits that can apply do and the others are reported; without it, one that cannot apply fails them all.",
		Example:     "Apply a suggestion: Anchor resolve finds its quote in the draft, then Edit text together replaces that range with the suggested text",
		Tags:        []string{"collaboration", "edit", "markdown", "replace", "yjs", "text"},
		Kinds:       []string{livenodes.KindAction, livenodes.KindTrigger, uinodes.KindHandler, cognition.KindFunction, cognition.KindAgent},
		Effects:     true, External: true,
		Config:      json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string","title":"Entity","description":"The entity type; the Entity pin wins"},"field":{"type":"string","title":"Field","description":"The markdown field; the Field pin wins"},"each":{"type":"boolean","title":"Each","description":"Apply the edits that can apply and report the others"}}}`),
		Sample:      cognition.Values{"applied": []any{true}, "problems": []any{""}, "live": false},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				{ID: "entity", Name: "Entity", Type: "string", Optional: true, Description: "The entity type, in place of its config's"},
				cognition.DataPin("id", "Record", "string", "The record's id"),
				{ID: "field", Name: "Field", Type: "string", Optional: true, Description: "The markdown field, in place of its config's"},
				cognition.DataPin("edits", "Edits", "json", "[{find, replace}] or [{start, end, find?, replace}], in order, each against the text the ones before left"),
				{ID: "each", Name: "Each", Type: "bool", Optional: true, Description: "In place of its config's"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""), cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("applied", "Applied", "json", "Per edit, whether it applied"),
				cognition.DataPin("problems", "Problems", "json", "Per edit, why it did not apply (empty when it did)"),
				cognition.DataPin("live", "Live", "bool", "Whether it went to an open room rather than straight to the field"),
				cognition.DataPin("error", "Error", "string", "Why it failed"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct {
				Entity, Field string
				Each          bool
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			entity, _ := cognition.Get[string](in, "entity")
			if entity == "" {
				entity = c.Entity
			}
			field, _ := cognition.Get[string](in, "field")
			if field == "" {
				field = c.Field
			}
			id, _ := cognition.Get[string](in, "id")
			each := c.Each
			if v, ok := in["each"].(bool); ok {
				each = v
			}
			var edits []collabservice.Edit
			raw, err := json.Marshal(in["edits"])
			if err == nil {
				err = json.Unmarshal(raw, &edits)
			}
			if err != nil {
				return cognition.Values{"error": "the edits are not a list of {find, replace} or {start, end, replace}"}, "failed", nil
			}
			svc := collab()
			if svc == nil {
				return cognition.Values{"error": "text cannot be edited together here"}, "failed", nil
			}
			ref, _ := json.Marshal(map[string]string{"entity": entity, "id": id, "field": field})
			resp := svc.EditDoc(collabservice.EditDocArgs{ServiceArgs: x.ServiceArgs, Kind: recordKind, Ref: ref, Edits: edits, Each: each})
			if resp.Common.Code >= http.StatusMultipleChoices {
				return cognition.Values{"error": resp.Common.Message}, "failed", nil
			}
			applied := make([]any, len(resp.Applied))
			problems := make([]any, len(resp.Problems))
			for i := range resp.Applied {
				applied[i] = resp.Applied[i]
				problems[i] = resp.Problems[i]
			}
			return cognition.Values{"applied": applied, "problems": problems, "live": resp.Live}, cognition.Then, nil
		},
	})
}
