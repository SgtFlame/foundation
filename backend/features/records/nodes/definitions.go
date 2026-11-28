package recordsnodes

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// definitionKinds are where the nodes that define entity types at runtime
// run (forge 35c-1): actions and handlers, which run once as the user.
// Not command graphs: a definition commits on the def stream, never in a
// record command's batch, and a command may be decided more than once.
var definitionKinds = []string{livenodes.KindAction, uinodes.KindHandler}

// definitionOuts are the outputs every definition node gives.
func definitionOuts(more ...cognition.Pin) []cognition.Pin {
	return append([]cognition.Pin{cognition.ExecPin("ok", "OK"), cognition.ExecPin("rejected", "Rejected"),
		cognition.DataPin("problems", "Problems", "json", "Why it was refused: [{path, message, fix}]"),
		cognition.DataPin("message", "Message", "string", "The refusal's first message")}, more...)
}

const publishConfig = `"publish":{"type":"boolean","title":"Publish","description":"Publish the change at once, so records convert and every open view shows it; true when left out"}`

// contributeDefinitions registers the nodes a running app defines its
// users' entity types with (forge 35c-1, Records.md › Built in forge
// 35c-1): each sends an entity.* command as the run's principal, only on
// a user-defined type the running graph's app owns.
func contributeDefinitions(reg *extensions.Registry, deps func() data.Deps) {
	node(reg, 720, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.createEntityType"}, Title: "Create entity type", Kinds: definitionKinds, Effects: true,
		Description: "Makes a user-defined entity type for the running app (entity.createUserDefined), held by a parent record, such as a Docs database's rows with the database's properties as fields. It is published at once. Only those who may define the parent record's types (record.define, a role's entity:<type>.define) may make or change it.",
		Example:     "Create entity type named Projects with parent {entity: database, id} and fields [{name: Name, type: text}], then Set field typeId on the database record",
		Examples:    cognition.ConfigExample("records.createEntityType", `{"name":"Projects"}`),
		Config:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","title":"Name","description":"The type's name when the Name input is unconnected"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				{ID: "name", Name: "Name", Type: "string", Optional: true, Description: "What people call it; the config's when unconnected"},
				cognition.DataPin("parent", "Parent", "json", "The record that holds it: {entity, id}"),
				{ID: "fields", Name: "Fields", Type: "json", Optional: true, Description: "Its fields, as entity.addField takes them: [{name, type, …}]"}},
			Out: definitionOuts(cognition.DataPin("entity", "Entity type", "string", "The new type's id (ud_<tsid>)")),
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Name string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if name, _ := cognition.Get[string](in, "name"); name != "" {
				c.Name = name
			}
			if !isApp(x.Owner) {
				return refused("only an app's graph defines entity types at runtime", "entity"), "rejected", nil
			}
			id := "ud_" + strconv.FormatUint(features.NewTSID(), 10)
			args := map[string]any{"name": c.Name, "owner": x.Owner, "parent": in["parent"]}
			if f := in["fields"]; f != nil {
				args["fields"] = f
			}
			out, pin := send(x, deps, "entity.createUserDefined", id, args)
			if pin == "ok" {
				out, pin = publish(x, deps, id)
			}
			out["entity"] = id
			return out, pin, nil
		},
	})
	field := func(order int, id, title, command, description, example string, in []cognition.Pin, argsOf func(cognition.Values) map[string]any) {
		node(reg, order, cognition.NodeType{
			Descriptor: extensions.Descriptor{ID: id}, Title: title, Kinds: definitionKinds, Effects: true,
			Description: description + " Only on a user-defined entity type of the running app (Create entity type); published at once unless Publish is off.",
			Example:     example,
			Examples:    cognition.ConfigExample(id, `{"publish":true}`),
			Config:      json.RawMessage(`{"type":"object","properties":{` + publishConfig + `}}`),
			Pins: cognition.Pins{
				In:  append([]cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("entity", "Entity type", "string", "The user-defined type's id")}, in...),
				Out: definitionOuts(),
			},
			Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
				c := struct{ Publish *bool }{}
				if err := x.Config(&c); err != nil {
					return nil, "", err
				}
				entity, _ := cognition.Get[string](in, "entity")
				if out := ownUserDefined(x, deps, entity); out != nil {
					return out, "rejected", nil
				}
				out, pin := send(x, deps, command, entity, argsOf(in))
				if pin == "ok" && (c.Publish == nil || *c.Publish) {
					out, pin = publish(x, deps, entity)
				}
				return out, pin, nil
			},
		})
	}
	field(722, "records.addEntityField", "Add field", "entity.addField",
		"Adds a field to a user-defined entity type (entity.addField): a database's new property.",
		"Add field {name: Status, type: enum, options} to the database's type",
		[]cognition.Pin{cognition.DataPin("field", "Field", "json", "The field, as entity.addField takes it: {name, type, …}")},
		func(in cognition.Values) map[string]any { return asMap(in["field"]) })
	field(724, "records.updateEntityField", "Update field", "entity.updateField",
		"Changes a field of a user-defined entity type by id (entity.updateField, a merge: null clears): renaming a database's property or changing its type, whose values convert when published.",
		"Update field status with {name: Stage}",
		[]cognition.Pin{cognition.DataPin("field", "Field", "string", "The field's id"),
			cognition.DataPin("changes", "Changes", "json", "What changes: {name?, type?, options?, …}")},
		func(in cognition.Values) map[string]any {
			a := asMap(in["changes"])
			a["field"], _ = cognition.Get[string](in, "field")
			return a
		})
	field(726, "records.removeEntityField", "Remove field", "entity.removeField",
		"Removes a field of a user-defined entity type (entity.removeField): hidden, its values kept, so Undo brings it back.",
		"Remove field due from the database's type",
		[]cognition.Pin{cognition.DataPin("field", "Field", "string", "The field's id")},
		func(in cognition.Values) map[string]any {
			f, _ := cognition.Get[string](in, "field")
			return map[string]any{"field": f}
		})
	node(reg, 728, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.publishEntityType"}, Title: "Publish entity type", Kinds: definitionKinds, Effects: true,
		Description: "Publishes a user-defined entity type of the running app at its current draft (entity.publish), so records convert and every open view shows its fields; for changes made with Publish off.",
		Example:     "Publish entity type after three Add field nodes with Publish off",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("entity", "Entity type", "string", "The user-defined type's id")},
			Out: definitionOuts(cognition.DataPin("version", "Version", "number", "The version published")),
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			entity, _ := cognition.Get[string](in, "entity")
			if out := ownUserDefined(x, deps, entity); out != nil {
				return out, "rejected", nil
			}
			out, pin := publish(x, deps, entity)
			return out, pin, nil
		},
	})
}

func isApp(owner string) bool { return len(owner) > 4 && owner[:4] == "app:" }

// draftOf reads a type's draft as the run sees it (its sandbox too).
func draftOf(x *cognition.Exec, deps func() data.Deps, id string) (entities.Draft, bool) {
	var d entities.Draft
	if !live.ValidToken(id) {
		return d, false
	}
	p := deps().For(x.ServiceArgs).Projections
	if p == nil {
		return d, false
	}
	v, ok, err := p.Get(x.Context(), x.Principal.TenantID, entities.DraftProjector, entities.TargetType+"."+id)
	if err != nil || !ok || json.Unmarshal(v.Value, &d) != nil {
		return d, false
	}
	return d, true
}

// ownUserDefined refuses a type that is not a user-defined type of the
// running graph's app; nil when it is one.
func ownUserDefined(x *cognition.Exec, deps func() data.Deps, id string) cognition.Values {
	d, ok := draftOf(x, deps, id)
	switch {
	case !ok:
		return refused(fmt.Sprintf("there is no entity type %q", id), "entity")
	case !d.Entity.UserDefined:
		return refused(fmt.Sprintf("%s is not a user-defined entity type: an app's own types change only in its draft", id), "entity")
	case d.Entity.Owner != x.Owner:
		return refused(fmt.Sprintf("%s belongs to %s, not this graph's app", id, d.Entity.Owner), "entity")
	}
	return nil
}

// publish publishes a type at its draft's rev, read once the drafts have
// folded the change just sent.
func publish(x *cognition.Exec, deps func() data.Deps, id string) (cognition.Values, string) {
	if p := deps().For(x.ServiceArgs).Projections; p != nil {
		_ = p.Settle(x.Context(), x.Principal.TenantID, entities.DraftProjector)
	}
	d, ok := draftOf(x, deps, id)
	if !ok {
		return refused(fmt.Sprintf("there is no entity type %q", id), "entity"), "rejected"
	}
	out, pin := send(x, deps, "entity.publish", id, map[string]any{"rev": d.Rev})
	out["version"] = d.Published + 1
	return out, pin
}

// send sends a command and turns its outcome into the nodes' outputs.
func send(x *cognition.Exec, deps func() data.Deps, command, target string, args map[string]any) (cognition.Values, string) {
	problems, fail := livenodes.Send(x, deps().Bus, command, target, args)
	if fail == nil {
		return cognition.Values{"problems": []live.Problem{}, "message": ""}, "ok"
	}
	msg := fail.Message
	if len(problems) > 0 {
		msg = problems[0].Message
	}
	if problems == nil {
		problems = []live.Problem{{Message: msg}}
	}
	return cognition.Values{"problems": plain(problems), "message": msg}, "rejected"
}

func refused(message, path string) cognition.Values {
	return cognition.Values{"problems": []live.Problem{{Path: path, Message: message}}, "message": message}
}

// asMap is an input object as a map, empty when it is not one.
func asMap(v any) map[string]any {
	m, ok := plain(v).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}
