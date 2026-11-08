// Package recordsnodes is the records' Cognition node family (forge
// 03i-TaskList, documentation/Records.md › Built in slice 23): command
// graphs create, change and delete records with the write nodes, whose
// events commit with the command's own as one atomic batch; every kind
// reads records with Get record, Query and Aggregate, and a data.query
// graph runs again when the records it read change. On record changed is
// the app.trigger event for records (runs come with forge 07-Email).
// Elsewhere than a command graph, a write is Send command with record.*.
package recordsnodes

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

const source = "records"

func node(reg *extensions.Registry, order int, t cognition.NodeType) {
	t.Order, t.Source = order, source
	if t.Category == "" {
		t.Category = "Records"
	}
	cognition.Nodes.Register(reg, t)
}

const entityConfig = `"entity":{"type":"string","title":"Entity type","description":"The entity type's id, e.g. task (entity.list)"}`

// plain turns a value into what graphs pass: maps, slices and scalars.
func plain(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// Contribute registers the record nodes; deps returns the records'
// dependencies once made.
func Contribute(reg *extensions.Registry, deps func() data.Deps) {
	contributeWrites(reg, deps)
	contributeReads(reg, deps)
	contributeFiles(reg, deps)
	node(reg, 700, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.onChanged"}, Title: "On record changed", Category: "Events", Event: true, Kinds: []string{livenodes.KindTrigger},
		Description: "Starts a trigger when a record of an entity type is created, changed or deleted (runs come with forge 07-Email)",
		Example:     "On record changed for task, when done turns true, to email the task's creator",
		Examples:    cognition.ConfigExample("records.onChanged", `{"entity":"task","events":["updated"]}`),
		Config:      json.RawMessage(`{"type":"object","properties":{` + entityConfig + `,"events":{"type":"array","title":"Events","items":{"type":"string","enum":["created","updated","deleted"]},"description":"The changes it starts on; every one when empty"}},"required":["entity"]}`),
		Pins: cognition.Pins{Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
			cognition.DataPin("record", "Record", "json", "The record after the change; nothing once deleted"),
			cognition.DataPin("old", "Old", "json", "The values the change replaced"),
			cognition.DataPin("event", "Event", "string", "created, updated or deleted")}},
	})
}

// write is one write node: the record command it decides, and how its
// inputs make the command's args.
type write struct {
	id, command, title, description, example string
	config                                   string // more config properties
	in                                       []cognition.Pin
	args                                     func(c map[string]any, in cognition.Values) map[string]any
}

func contributeWrites(reg *extensions.Registry, deps func() data.Deps) {
	idPin := func(description string) cognition.Pin { return cognition.DataPin("id", "ID", "string", description) }
	expectPin := cognition.Pin{ID: "expect", Name: "Expect", Type: "number", Optional: true, Description: "The record's _seq as last shown: refused with record_changed if it moved since"}
	fieldConfig := `,"field":{"type":"string","title":"Field","description":"The list-like field's id"}`
	withExpect := func(a map[string]any, in cognition.Values) map[string]any {
		if e, ok := in["expect"]; ok && e != nil {
			a["expect"] = e
		}
		return a
	}
	writes := []write{
		{id: "records.create", command: "record.create", title: "Create record",
			description: "Creates a record of an entity type with its values by field id; fields left out get their default. Its event commits with the command's.",
			example:     "Create record task with values {title, due} from the args",
			in: []cognition.Pin{{ID: "id", Name: "ID", Type: "string", Optional: true, Description: "The new record's id; a new one when unconnected"},
				{ID: "values", Name: "Values", Type: cognition.Any, Optional: true, Description: "Field id → value"}},
			args: func(_ map[string]any, in cognition.Values) map[string]any {
				values := in["values"]
				if values == nil {
					values = map[string]any{}
				}
				return map[string]any{"values": values}
			}},
		{id: "records.update", command: "record.update", title: "Update record",
			description: "Sets some of a record's values by field id (a patch; null clears one). Its event commits with the command's.",
			example:     "Update record task at the target with {done: !state.done}",
			in:          []cognition.Pin{idPin("The record's id"), {ID: "set", Name: "Set", Type: cognition.Any, Description: "Field id → new value"}, expectPin},
			args: func(_ map[string]any, in cognition.Values) map[string]any {
				return withExpect(map[string]any{"set": in["set"]}, in)
			}},
		{id: "records.delete", command: "record.delete", title: "Delete record",
			description: "Deletes a record; its events keep its history. Its event commits with the command's.",
			example:     "Delete record task for each done task the Query found",
			in:          []cognition.Pin{idPin("The record's id"), expectPin},
			args:        func(_ map[string]any, in cognition.Values) map[string]any { return withExpect(map[string]any{}, in) }},
		{id: "records.addTo", command: "record.addTo", title: "Add to",
			description: "Adds items to a list-like field (multiEnum, tags, list or a to-many relation), keeping what is there. Its event commits with the command's.",
			example:     "Add to task's tags the item \"urgent\"", config: fieldConfig,
			in: []cognition.Pin{idPin("The record's id"), {ID: "items", Name: "Items", Type: cognition.Any, Description: "The items to add, a list"}, expectPin},
			args: func(c map[string]any, in cognition.Values) map[string]any {
				return withExpect(map[string]any{"field": c["field"], "items": in["items"]}, in)
			}},
		{id: "records.removeFrom", command: "record.removeFrom", title: "Remove from",
			description: "Removes items from a list-like field, leaving the others. Its event commits with the command's.",
			example:     "Remove from task's tags the item \"urgent\"", config: fieldConfig,
			in: []cognition.Pin{idPin("The record's id"), {ID: "items", Name: "Items", Type: cognition.Any, Description: "The items to remove, a list"}, expectPin},
			args: func(c map[string]any, in cognition.Values) map[string]any {
				return withExpect(map[string]any{"field": c["field"], "items": in["items"]}, in)
			}},
	}
	for i, w := range writes {
		w := w
		required, example := `"entity"`, `{"entity":"task"}`
		if w.config != "" {
			required += `,"field"`
			example = `{"entity":"task","field":"tags"}`
		}
		node(reg, 600+i*10, cognition.NodeType{
			Descriptor: extensions.Descriptor{ID: w.id}, Title: w.title, Kinds: []string{livenodes.KindCommand},
			Description: w.description + " Checked as " + w.command + " is, and refused with the command's problems.",
			Example:     w.example,
			Examples:    cognition.ConfigExample(w.id, example),
			Config:      json.RawMessage(`{"type":"object","properties":{` + entityConfig + w.config + `},"required":[` + required + `]}`),
			Pins: cognition.Pins{
				In:  append([]cognition.Pin{cognition.ExecPin(cognition.Then, "")}, w.in...),
				Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("id", "ID", "string", "The record's id")},
			},
			Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
				var c map[string]any
				if err := x.Config(&c); err != nil {
					return nil, "", err
				}
				h, ok := x.Host.(*livenodes.CommandHost)
				if !ok {
					return nil, "", fmt.Errorf("%s runs only in a command graph; elsewhere, send %s with Send command", w.title, w.command)
				}
				d := live.Decision{ServiceArgs: x.ServiceArgs}
				if h.Decision != nil {
					d = *h.Decision
				}
				a := w.args(c, in)
				a["entity"] = c["entity"]
				raw, _ := json.Marshal(a)
				id, _ := cognition.Get[string](in, "id")
				events, id, problems := data.Write(d, deps(), w.command, id, raw)
				if problems != nil {
					// Refused as Reject is: nothing commits, and the run ends
					h.Problems = append(h.Problems, problems...)
					return nil, "", nil
				}
				h.Events = append(h.Events, events...)
				return cognition.Values{"id": id}, cognition.Then, nil
			},
		})
	}
}

// watch notes what a query graph read, so it runs again when that
// changes: the records of the types it read (a pattern) and their
// published versions, from the position the projector had folded before
// the read.
func watch(x *cognition.Exec, d data.Deps, types ...string) {
	q, ok := x.Host.(*livenodes.QueryHost)
	if !ok {
		return
	}
	if pos, err := d.Projections.Folded(x.Context(), x.Principal.TenantID, data.Projector); err == nil {
		q.Pos = q.Pos.Merge(pos)
	}
	for _, t := range types {
		if t == records.UserEntity {
			continue
		}
		reads := []livenodes.Read{
			{Projector: data.Projector, Key: t + ".*", Pattern: true},
			{Projector: entities.PublishedProjector, Key: entities.TargetType + "." + t},
		}
		for _, r := range reads {
			if !slices.Contains(q.Reads, r) {
				q.Reads = append(q.Reads, r)
			}
		}
	}
}

// related is the entity types a query's expansions read.
func related(x *cognition.Exec, d data.Deps, entity string, expand []string) []string {
	out := []string{entity}
	if len(expand) == 0 {
		return out
	}
	e, problems := data.Entity(x.ServiceArgs, d.Projections, entity)
	if problems != nil {
		return out
	}
	for _, id := range expand {
		if f, _, ok := e.FieldByID(id); ok && f.Relation != nil && !slices.Contains(out, f.Relation.Entity) {
			out = append(out, f.Relation.Entity)
		}
	}
	return out
}

// request is a read node's request: its config, with the request input's
// fields over it.
func request[T any](x *cognition.Exec, in cognition.Values, pin string) (T, error) {
	var c map[string]any
	var out T
	if err := x.Config(&c); err != nil {
		return out, err
	}
	if c == nil {
		c = map[string]any{}
	}
	if m, ok := in[pin].(map[string]any); ok {
		for k, v := range m {
			c[k] = v
		}
	}
	raw, _ := json.Marshal(c)
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("the %s is not valid: %v", pin, err)
	}
	return out, nil
}

const queryProps = `"where":{"type":"object","title":"Where","description":"Field id → value the records must have"},
  "filter":{"type":"object","title":"Filter","description":"{field, op, value}, or {and: [filters]} or {or: [filters]}"}`

func contributeReads(reg *extensions.Registry, deps func() data.Deps) {
	node(reg, 650, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.get"}, Title: "Get record", Volatile: true,
		Description: "Reads one record by id, as records.get gives it (nothing when there is none or a row rule hides it); a query graph runs again when it changes",
		Example:     "Get record task at the event's id, to check it is not done",
		Examples:    cognition.ConfigExample("records.get", `{"entity":"task"}`),
		Config:      json.RawMessage(`{"type":"object","properties":{` + entityConfig + `},"required":["entity"]}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.DataPin("id", "ID", "string", "The record's id")},
			Out: []cognition.Pin{cognition.DataPin("record", "Record", "json", "Its values by field id, and _id, _seq and its other own data"),
				cognition.DataPin("exists", "Exists", "bool", "Whether there is such a record")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Entity string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			id, _ := cognition.Get[string](in, "id")
			d := deps()
			if q, ok := x.Host.(*livenodes.QueryHost); ok {
				q.Reads = append(q.Reads, livenodes.Read{Projector: data.Projector, Key: data.Key(c.Entity, id)},
					livenodes.Read{Projector: entities.PublishedProjector, Key: entities.TargetType + "." + c.Entity})
			}
			r, pos, err := data.Get(x.ServiceArgs, d, c.Entity, id)
			if err != nil {
				return nil, "", err
			}
			if q, ok := x.Host.(*livenodes.QueryHost); ok {
				q.Pos = q.Pos.Merge(pos)
			}
			if r == nil {
				return cognition.Values{"record": nil, "exists": false}, "", nil
			}
			return cognition.Values{"record": plain(r), "exists": true}, "", nil
		},
	})
	node(reg, 660, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.query"}, Title: "Query", Volatile: true,
		Description: "Reads a sorted page of an entity type's records, as records.find does, configured here or given a query object whose fields win; a query graph runs again, debounced, when the type's records change. For rows a page displays, bind records.find instead: it sends row changes.",
		Example:     "Query task where {done: true}, to delete each in a Clear done command",
		Config: json.RawMessage(`{"type":"object","properties":{` + entityConfig + `,` + queryProps + `,
  "sort":{"type":"array","title":"Sort","items":{"type":"object","properties":{"field":{"type":"string"},"desc":{"type":"boolean"}},"required":["field"]}},
  "limit":{"type":"integer","title":"Limit","description":"The page's size; every match when left out"},
  "expand":{"type":"array","title":"Expand","items":{"type":"string"},"description":"Relation fields to follow one level, under _expanded.<field>"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{{ID: "query", Name: "Query", Type: cognition.Any, Optional: true, Description: "{entity, where, filter, sort, limit, cursor, expand}, over the config"}},
			Out: []cognition.Pin{cognition.DataPin("records", "Records", "json", "The page's records, in order"),
				cognition.DataPin("next", "Next", "string", "The cursor of the page after; empty on the last")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			q, err := request[data.Query](x, in, "query")
			if err != nil {
				return nil, "", err
			}
			d := deps()
			watch(x, d, related(x, d, q.Entity, q.Expand)...)
			res, err := data.Find(x.ServiceArgs, d, q)
			if err != nil {
				return nil, "", err
			}
			rows := plain(res.Records)
			if rows == nil {
				rows = []any{}
			}
			return cognition.Values{"records": rows, "next": res.Next}, "", nil
		},
	})
	node(reg, 670, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.aggregate"}, Title: "Aggregate", Volatile: true,
		Description: "Counts, sums, averages, or finds the least or greatest of an entity type's records, in groups, as records.aggregate does; configured here or given a request whose fields win. A query graph runs again, debounced, when the type's records change.",
		Example:     "Aggregate task where {done: false} with count, for an \"N open\" label",
		Config: json.RawMessage(`{"type":"object","properties":{` + entityConfig + `,` + queryProps + `,
  "groupBy":{"type":"string","title":"Group by","description":"A field id; one group of every record when left out"},
  "aggregates":{"type":"array","title":"Aggregates","items":{"type":"object","properties":{"op":{"type":"string","enum":["count","sum","avg","min","max"]},"field":{"type":"string"},"as":{"type":"string"}},"required":["op"]}}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{{ID: "request", Name: "Request", Type: cognition.Any, Optional: true, Description: "{entity, where, filter, groupBy, aggregates}, over the config"}},
			Out: []cognition.Pin{cognition.DataPin("groups", "Groups", "json", "[{key, values}], ordered by key; values are keyed by each aggregate's as (count, sum_<field> …)"),
				cognition.DataPin("values", "Values", "json", "The first group's values: the totals when there is no group by")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			a, err := request[data.Aggregation](x, in, "request")
			if err != nil {
				return nil, "", err
			}
			if len(a.Aggregates) == 0 {
				a.Aggregates = []data.Aggregate{{Op: "count"}}
			}
			d := deps()
			watch(x, d, a.Entity)
			s, err := data.Summarize(x.ServiceArgs, d, a)
			if err != nil {
				return nil, "", err
			}
			groups, _ := plain(s.Groups).([]any)
			if groups == nil {
				groups = []any{}
			}
			var values any = map[string]any{}
			if len(groups) > 0 {
				values = groups[0].(map[string]any)["values"]
			} else if a.GroupBy == "" {
				// No records: the totals of nothing
				empty := map[string]any{}
				for _, g := range a.Aggregates {
					as := g.As
					if as == "" {
						as = g.Op
						if g.Field != "" {
							as += "_" + g.Field
						}
					}
					if g.Op == "count" || g.Op == "sum" {
						empty[as] = 0
					} else {
						empty[as] = nil
					}
				}
				values = empty
			}
			return cognition.Values{"groups": groups, "values": values}, "", nil
		},
	})
}
