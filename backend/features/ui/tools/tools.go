// Package uitools contributes the server-driven UI's chat tools: the
// component catalog, one component's metadata, and validating a tree
// (documentation/ServerUI.md › Tools). Published prefabs are components
// too, as prefab.<id> (forge 04b).
package uitools

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// published reads a prefab's last published version, or version, as a
// component.
func published(call tools.Call, subs func() *subscribe.Subscriptions, id string, version int) (ui.Component, bool) {
	if subs == nil || subs() == nil {
		return ui.Component{}, false
	}
	params := map[string]any{"id": id, "published": true}
	if version > 0 {
		params = map[string]any{"id": id, "version": version}
	}
	raw, _ := json.Marshal(params)
	data, _, fail := subs().Once(call.ServiceArgs, "prefab.describe", raw)
	if fail != nil {
		return ui.Component{}, false
	}
	raw, _ = json.Marshal(data)
	var v struct{ Prefab ui.Prefab }
	if json.Unmarshal(raw, &v) != nil {
		return ui.Component{}, false
	}
	return ui.PrefabComponent(v.Prefab), true
}

// Contribute registers the ui tools. subs returns the subscriptions once
// made, for the published prefabs; nil leaves them out.
func Contribute(reg *extensions.Registry, subs func() *subscribe.Subscriptions) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "ui.listComponents", Source: "ui"}, Title: "List UI components", Effect: tools.EffectNone,
		Description: "Lists the UI components a page's tree may use, each with its id, title, category, description, tags and events: the primitives, and the published prefabs as prefab.<id> (category Prefabs). Describe one with ui.describeComponent for its props schema, events, slots and examples.",
		Run: func(call tools.Call) (tools.Result, error) {
			var list []map[string]any
			for _, c := range ui.Components.All(reg) {
				var events []string
				for _, e := range c.Events {
					events = append(events, e.Name)
				}
				list = append(list, map[string]any{"id": c.ID, "title": c.Title, "category": c.Category, "description": c.Description, "tags": c.Tags, "events": events, "children": c.Children})
			}
			// Published prefabs, by their summaries
			if subs != nil && subs() != nil {
				if data, _, fail := subs().Once(call.ServiceArgs, "prefab.list", json.RawMessage(`{}`)); fail == nil {
					raw, _ := json.Marshal(data)
					var byKey map[string]struct {
						ID, Name, Description, Owner string
						Published                    int
					}
					_ = json.Unmarshal(raw, &byKey)
					for _, p := range byKey {
						if p.Published > 0 {
							list = append(list, map[string]any{"id": ui.PrefabPrefix + p.ID, "title": p.Name, "category": "Prefabs", "description": p.Description, "tags": []string{p.Owner}})
						}
					}
				}
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
			if id, version, isPrefab := ui.InstanceOf(args.ID); isPrefab {
				c, ok = published(call, subs, id, version)
				c.ID = args.ID
			}
			if !ok {
				return tools.Result{}, fmt.Errorf("there is no component %q; list them with ui.listComponents", args.ID)
			}
			return tools.Result{Content: map[string]any{"id": c.ID, "title": c.Title, "description": c.Description, "category": c.Category, "props": c.Props,
				"events": c.Events, "slots": c.Slots, "children": c.Children, "examples": c.Examples}, Summary: c.Title}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "ui.previewTree", Source: "ui"}, Title: "Check a UI tree", Effect: tools.EffectNone,
		Description: "Validates a component tree ({type, key, props, bind, on, children, slots}) against the catalog, published prefabs included, and returns its problems, each with the key path of the node: unknown components, invalid props, an sx with more than theme tokens, missing or repeated keys, events a component does not emit. For a prefab with its data, use prefab.preview.",
		Input:       json.RawMessage(`{"type":"object","properties":{"tree":{"type":"object","description":"The root node"}},"required":["tree"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct{ Tree ui.Node }](call)
			if err != nil {
				return tools.Result{}, err
			}
			catalog := ui.Catalog(reg)
			var walk func(n *ui.Node)
			walk = func(n *ui.Node) {
				if id, version, ok := ui.InstanceOf(n.Type); ok {
					if c, ok := published(call, subs, id, version); ok {
						catalog[n.Type] = c
					}
				}
				for _, c := range n.Children {
					walk(c)
				}
				for _, list := range n.Slots {
					for _, c := range list {
						walk(c)
					}
				}
			}
			walk(&args.Tree)
			problems := ui.Validate(catalog, &args.Tree)
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
