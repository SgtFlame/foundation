package uitools

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// ContributePrefabs registers the prefab tools: describing and listing
// prefabs, and the types their interfaces use. Every prefab.* command is
// already a tool of its own (live/tools). subs returns the subscriptions
// once made.
func ContributePrefabs(reg *extensions.Registry, subs func() *subscribe.Subscriptions) {
	once := func(call tools.Call, query string) (any, error) {
		data, _, fail := subs().Once(call.ServiceArgs, query, call.Args)
		if fail != nil {
			return nil, fmt.Errorf("%s", fail.Message)
		}
		return data, nil
	}

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "prefab.describe", Source: "ui"}, Title: "Describe a prefab", Effect: tools.EffectRead,
		Description: "Describes a prefab (a reusable component): its props, events, slots, state and queries, its template (nodes {type, key, props, bind, on, children, slots}, found by key paths such as root/row/title), its graphs and preview data, and its draft's rev for prefab.publish. With published, its last published version; with version, that one.",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"published":{"type":"boolean"},"version":{"type":"integer"}},"required":["id"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			data, err := once(call, "prefab.describe")
			if err != nil {
				return tools.Result{}, err
			}
			return tools.Result{Content: map[string]any{"prefab": data}, Summary: "Described a prefab"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "prefab.list", Source: "ui"}, Title: "List prefabs", Effect: tools.EffectRead,
		Description: "Lists the tenant's prefabs: id, name, description, owner, rev, the published version and whether the draft changed since. owner narrows it to one app's (app:<appId>) or the tenant library's (tenant); search keeps those whose name or description holds the text. A prefab is used in a tree as a node of type prefab.<id>.",
		Input:       json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"search":{"type":"string"}}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			data, err := once(call, "prefab.list")
			if err != nil {
				return tools.Result{}, err
			}
			// The query's result is keyed by "prefab.<id>"; a model gets a list
			raw, _ := json.Marshal(data)
			var byKey map[string]any
			_ = json.Unmarshal(raw, &byKey)
			list := []any{}
			for _, v := range byKey {
				list = append(list, v)
			}
			return tools.Result{Content: map[string]any{"prefabs": list}, Summary: strconv.Itoa(len(list)) + " prefabs"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "prefab.preview", Source: "ui"}, Title: "Preview a prefab", Effect: tools.EffectRead,
		Description: "Evaluates a prefab with props (its preview data when left out): its last published version, or with draft its draft. Returns the evaluated tree of primitives, as a browser would get it, and its problems, each with the key path of the node it is at (an unknown prefab, a missing required prop, a binding to nothing). Its queries are read once and its logic does not run.",
		Input:       json.RawMessage(`{"type":"object","properties":{"prefab":{"type":"string","description":"The prefab's id"},"props":{"type":"object"},"draft":{"type":"boolean"}},"required":["prefab"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Prefab string
				Props  map[string]any
				Draft  bool
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			tree, problems, fail := ui.Preview(call.ServiceArgs, ui.Options{Catalog: ui.Catalog(reg), Subs: subs()}, a.Prefab, a.Props, a.Draft)
			if fail != nil {
				return tools.Result{}, fmt.Errorf("%s", fail.Message)
			}
			summary := "No problems"
			if len(problems) > 0 {
				summary = strconv.Itoa(len(problems)) + " problems"
			}
			return tools.Result{Content: map[string]any{"tree": tree, "problems": problems, "valid": len(problems) == 0}, Summary: summary}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "prefab.listTypes", Source: "ui"}, Title: "List prefab types", Effect: tools.EffectNone,
		Description: "Lists the types a prefab's props, state and event payload fields can have.",
		Input:       json.RawMessage(`{"type":"object","properties":{}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			return tools.Result{Content: map[string]any{"types": ui.PrefabTypes}, Summary: strconv.Itoa(len(ui.PrefabTypes)) + " types"}, nil
		},
	})
}
