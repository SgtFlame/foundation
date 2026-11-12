// Package recordstools contributes the entity type tools: describing and
// listing entity types and the field types. Every entity.* command is
// already a tool of its own (live/tools).
package recordstools

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Contribute registers the tools; subs returns the subscriptions once made.
func Contribute(reg *extensions.Registry, subs func() *subscribe.Subscriptions) {
	once := func(call tools.Call, query string) (any, error) {
		data, _, fail := subs().Once(call.ServiceArgs, query, call.Args)
		if fail != nil {
			return nil, fmt.Errorf("%s", fail.Message)
		}
		return data, nil
	}

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "entity.describe", Source: "records"}, Title: "Describe an entity type", Effect: tools.EffectRead,
		Description: "Describes an entity type (a schema for records): its fields with their ids, names, types and settings (removed ones with hidden: true), its title field, indexes and row rules, and its draft's rev for entity.publish. With published, its last published version.",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The entity type's id"},"published":{"type":"boolean"}},"required":["id"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			data, err := once(call, "entity.describe")
			if err != nil {
				return tools.Result{}, err
			}
			return tools.Result{Content: map[string]any{"entityType": data}, Summary: "Described an entity type"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "entity.list", Source: "records"}, Title: "List entity types", Effect: tools.EffectRead,
		Description: "Lists the tenant's entity types: id, name, description, owner, how many fields, rev and the published version. owner narrows it to one app's (app:<appId>) or the shared ones (tenant).",
		Input:       json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"}}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			data, err := once(call, "entity.list")
			if err != nil {
				return tools.Result{}, err
			}
			// The query's result is keyed by "entity.<id>"; a model gets a list
			raw, _ := json.Marshal(data)
			var byKey map[string]any
			_ = json.Unmarshal(raw, &byKey)
			list := []any{}
			for _, v := range byKey {
				list = append(list, v)
			}
			return tools.Result{Content: map[string]any{"entityTypes": list}, Summary: strconv.Itoa(len(list)) + " entity types"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "entity.listFieldTypes", Source: "records"}, Title: "List field types", Effect: tools.EffectNone,
		Description: "Lists the types a field of an entity type can have, by group (basic, choices, references, structured, computed), with what each holds and needs.",
		Input:       json.RawMessage(`{"type":"object","properties":{}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			return tools.Result{Content: map[string]any{"fieldTypes": records.FieldTypes}, Summary: strconv.Itoa(len(records.FieldTypes)) + " field types"}, nil
		},
	})
}
