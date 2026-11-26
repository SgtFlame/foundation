// Package apptools registers the app tools: describing and listing apps.
// Every app.* command is already a tool of its own (live/tools).
package apptools

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Contribute registers app.describe and app.list; subs returns the
// subscriptions once made.
func Contribute(reg *extensions.Registry, subs func() *subscribe.Subscriptions) {
	once := func(call tools.Call, query string) (any, error) {
		data, _, fail := subs().Once(call.ServiceArgs, query, call.Args)
		if fail != nil {
			return nil, fmt.Errorf("%s", fail.Message)
		}
		return data, nil
	}

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "app.describe", Source: "apps"}, Title: "Describe an app", Effect: tools.EffectRead,
		Description: "Describes an application's draft: its manifest (pages as routes to prefabs, nav, home, frame, editor kinds, entity types, commands and queries with their graphs, roles with actions and row rules, settings, triggers, requires), its rev, and its parts: the prefabs, graphs and entity types it names or owns, each with its name, published version, and whether it is missing. Change it with the app.* commands; list items are named by their key (a page's route, a role's id, a command's type, a query's name).",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			data, err := once(call, "app.describe")
			if err != nil {
				return tools.Result{}, err
			}
			return tools.Result{Content: map[string]any{"app": data}, Summary: "Described an app"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "app.list", Source: "apps"}, Title: "List apps", Effect: tools.EffectRead,
		Description: "Lists the tenant's applications: id, name, icon, description, rev and how many pages. search keeps those whose name or description holds the text.",
		Input:       json.RawMessage(`{"type":"object","properties":{"search":{"type":"string"}}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			data, err := once(call, "app.list")
			if err != nil {
				return tools.Result{}, err
			}
			// The query's result is keyed by "app.<id>"; a model gets a list
			raw, _ := json.Marshal(data)
			var byKey map[string]any
			_ = json.Unmarshal(raw, &byKey)
			list := []any{}
			for _, v := range byKey {
				list = append(list, v)
			}
			return tools.Result{Content: map[string]any{"apps": list}, Summary: strconv.Itoa(len(list)) + " apps"}, nil
		},
	})
}
