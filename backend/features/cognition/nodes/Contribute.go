// Package cognitionnodes contributes the Cognition Engine's own node types
// (Flow: Branch, Return) and data types (string, number, bool, json, any).
// Features contribute their own: Chat's events in conversation/nodes,
// Chronicle's steps in chronicle/nodes.
package cognitionnodes

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

const source = "cognition"

// Contribute registers the core node and data types.
func Contribute(reg *extensions.Registry) {
	for i, t := range []cognition.DataType{
		{Descriptor: extensions.Descriptor{ID: "string"}, Color: "#e91e8c", Description: "Text"},
		{Descriptor: extensions.Descriptor{ID: "number"}, Color: "#4caf50", Description: "A number"},
		{Descriptor: extensions.Descriptor{ID: "bool"}, Color: "#c62828", Description: "True or false"},
		{Descriptor: extensions.Descriptor{ID: "json"}, Color: "#ff9800", Description: "Any JSON value"},
		{Descriptor: extensions.Descriptor{ID: cognition.Any}, Color: "#9e9e9e", Description: "Any type: takes the type of what it is joined to"},
	} {
		t.Order, t.Source = i, source
		cognition.Types.Register(reg, t)
	}

	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "flow.branch", Order: 100, Source: source},
		Title:       "Branch",
		Category:    "Flow",
		Description: "Continues by True or False, as the condition is",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("condition", "Condition", "bool", "")},
			Out: []cognition.Pin{cognition.ExecPin("true", "True"), cognition.ExecPin("false", "False")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			ok, err := cognition.Get[bool](in, "condition")
			if err != nil {
				return nil, "", err
			}
			if ok {
				return nil, "true", nil
			}
			return nil, "false", nil
		},
	})

	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "flow.return", Order: 900, Source: source},
		Title:       "Return",
		Category:    "Flow",
		Description: "Ends the run with its inputs as the run's outputs",
		Config:      json.RawMessage(returnSchema),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			outputs, err := cognition.DeclaredPins(config)
			if err != nil {
				return cognition.Pins{}, err
			}
			for i := range outputs {
				outputs[i].Optional = true
			}
			return cognition.Pins{In: append([]cognition.Pin{cognition.ExecPin(cognition.Then, "")}, outputs...)}, nil
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			x.Return(in)
			return nil, "", nil
		},
	})
}

const returnSchema = `{
  "type": "object",
  "properties": {
    "outputs": {"type": "array", "title": "Outputs", "items": {"type": "object", "properties": {
      "id": {"type": "string", "title": "ID"},
      "name": {"type": "string", "title": "Name"},
      "type": {"type": "string", "title": "Type"}
    }}}
  }
}`
