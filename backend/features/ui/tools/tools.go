// Package uitools contributes the server-driven UI's chat tools: the
// component catalog, one component's metadata, and validating a tree
// (documentation/ServerUI.md › Tools).
package uitools

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Contribute registers the ui tools.
func Contribute(reg *extensions.Registry) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "ui.listComponents", Source: "ui"}, Title: "List UI components", Effect: tools.EffectNone,
		Description: "Lists the primitive UI components a page's tree may use, each with its id, title, category, description, tags and events. Describe one with ui.describeComponent for its props schema and examples.",
		Run: func(call tools.Call) (tools.Result, error) {
			var list []map[string]any
			for _, c := range ui.Components.All(reg) {
				var events []string
				for _, e := range c.Events {
					events = append(events, e.Name)
				}
				list = append(list, map[string]any{"id": c.ID, "title": c.Title, "category": c.Category, "description": c.Description, "tags": c.Tags, "events": events, "children": c.Children})
			}
			return tools.Result{Content: map[string]any{"components": list}, Summary: fmt.Sprintf("%d components", len(list))}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "ui.describeComponent", Source: "ui"}, Title: "Describe a UI component", Effect: tools.EffectNone,
		Description: "Returns one UI component's full metadata: its props JSON Schema, events with their payloads, slots and example trees.",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The component, e.g. mui.button"}},"required":["id"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct{ ID string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			c, ok := ui.Catalog(reg)[args.ID]
			if !ok {
				return tools.Result{}, fmt.Errorf("there is no component %q; list them with ui.listComponents", args.ID)
			}
			return tools.Result{Content: map[string]any{"id": c.ID, "title": c.Title, "description": c.Description, "category": c.Category, "props": c.Props,
				"events": c.Events, "slots": c.Slots, "children": c.Children, "examples": c.Examples}, Summary: c.Title}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "ui.previewTree", Source: "ui"}, Title: "Check a UI tree", Effect: tools.EffectNone,
		Description: "Validates a component tree ({type, key, props, bind, on, children, slots}) against the catalog and returns its problems, each with the key path of the node: unknown components, invalid props, missing or repeated keys, events a component does not emit.",
		Input:       json.RawMessage(`{"type":"object","properties":{"tree":{"type":"object","description":"The root node"}},"required":["tree"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct{ Tree ui.Node }](call)
			if err != nil {
				return tools.Result{}, err
			}
			problems := ui.Validate(ui.Catalog(reg), &args.Tree)
			if problems == nil {
				problems = []ui.Problem{}
			}
			summary := "No problems"
			if len(problems) > 0 {
				summary = fmt.Sprintf("%d problems", len(problems))
			}
			return tools.Result{Content: map[string]any{"problems": problems, "valid": len(problems) == 0}, Summary: summary}, nil
		},
	})
}
