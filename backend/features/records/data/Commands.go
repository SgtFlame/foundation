package data

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordssettings "github.com/SgtFlame/foundation/backend/features/records/settings"
	"github.com/nats-io/nats.go/jetstream"
)

// Projections are what the records read: the projections, following the
// def stream for the converter and indexer, and the side bucket the
// indexes are kept in (live/project.Projections).
type Projections interface {
	live.Store
	Follow(ctx context.Context, tenant uint64, area live.Area, name string, subjects []string, fn func(live.Event) error) error
	Updates(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *live.Entry, error)
	Side(ctx context.Context, tenant uint64, name string) (jetstream.KeyValue, error)
	Rekeep(ctx context.Context, tenant uint64, projector, key string, fn func(value json.RawMessage, kept []string) ([]string, error)) error
	Settle(ctx context.Context, tenant uint64, projector string) error
	Folded(ctx context.Context, tenant uint64, projector string) (live.Position, error)
	Context() context.Context
}

// Deps are what the commands, queries and converter need, made after the
// registry.
type Deps struct {
	Projections Projections
	Bus         *bus.Bus // the converter's record.convert commands
	// Settings reads the records section as the principal sees it.
	Settings func(args features.ServiceArgs) recordssettings.Records
	// Users reads the tenant's users an expansion of a relation to user
	// names, by id; those not found, or not in the tenant, are left out.
	// Without it, such a relation expands to nothing.
	Users func(tenant uint64, ids []uint64) map[uint64]User
}

// User is a tenant user as an expansion gives them.
type User struct {
	Username    string
	DisplayName string
}

func (d Deps) settings(args features.ServiceArgs) recordssettings.Records {
	if d.Settings == nil {
		return recordssettings.Defaults
	}
	return d.Settings(args)
}

// Entity is an entity type's published version, which records follow; ok
// is false when it has none. A type with only a draft gets a problem
// telling to publish it.
func Entity(args features.ServiceArgs, store live.Store, id string) (records.EntityType, []live.Problem) {
	var e records.EntityType
	if !live.ValidToken(id) {
		return e, problem("args.entity", fmt.Sprintf("%q is not an entity type id", id), "list the entity types with entity.list")
	}
	v, ok, err := store.Get(args.Context(), args.Principal.TenantID, entities.PublishedProjector, entities.TargetType+"."+id)
	if err != nil {
		return e, problem("", "the entity type cannot be read: "+err.Error(), "")
	}
	if !ok {
		if _, draft, _ := store.Get(args.Context(), args.Principal.TenantID, entities.DraftProjector, entities.TargetType+"."+id); draft {
			return e, problem("args.entity", fmt.Sprintf("entity type %s has not been published", id), "publish it with entity.publish; records follow its published version")
		}
		return e, problem("args.entity", fmt.Sprintf("there is no entity type %q", id), "list the entity types with entity.list")
	}
	if err := json.Unmarshal(v.Value, &e); err != nil {
		return e, problem("", "the entity type cannot be read: "+err.Error(), "")
	}
	if e.Hidden {
		return e, problem("args.entity", fmt.Sprintf("entity type %s is deleted", id), "restore it with entity.restore first")
	}
	return e, nil
}

func problem(path, message, fix string) []live.Problem {
	return []live.Problem{{Path: path, Message: message, Fix: fix}}
}

// decision is one record command's view of its record and entity type.
type decision struct {
	live.Decision
	deps   Deps
	entity records.EntityType
	record Record // nil when there is none
}

// field is a visible field of the entity type, for a value at path.
func (c *decision) field(id, path string) (records.Field, []live.Problem) {
	f, _, ok := c.entity.FieldByID(id)
	if !ok || f.Hidden {
		return f, problem(path, fmt.Sprintf("%s has no field %q", c.entity.Name, id), "describe it with entity.describe {id: \""+c.entity.ID+"\", published: true} for its fields' ids")
	}
	return f, nil
}

// value checks one value for a field.
func (c *decision) value(id string, v json.RawMessage, path string) (json.RawMessage, []live.Problem) {
	f, problems := c.field(id, path)
	if problems != nil {
		return nil, problems
	}
	out, err := records.CheckValue(f, v)
	if err != nil {
		return nil, problem(path, f.Name+": "+err.Error(), "")
	}
	if out == nil && f.Required {
		return nil, problem(path, f.Name+" is required", "give it a value")
	}
	return out, nil
}

func (c *decision) expect(a struct{ Expect uint64 }) []live.Problem {
	if a.Expect != 0 && a.Expect != c.record.Seq() {
		return []live.Problem{{Code: "record_changed", Path: "args.expect", Message: fmt.Sprintf("the record changed since %d: it is at %d", a.Expect, c.record.Seq()),
			Fix: "reload it and decide again, or send without expect to overwrite"}}
	}
	return nil
}

// rules checks the row rule of the command's action (record.create,
// update or delete): create's on the new record, update's on the record
// before and after the change, and delete's on the record before.
func (c *decision) rules(action string, events []live.NewEvent) []live.Problem {
	verb := strings.TrimPrefix(action, "record.")
	rule := c.entity.Rule(verb)
	if rule == nil || c.Principal.Kind == features.PrincipalService {
		return nil
	}
	refused := func(which string) []live.Problem {
		return []live.Problem{{Code: "row_rule", Path: "target", Message: fmt.Sprintf("%s's %s rule (%s) refuses this %s", c.entity.Name, verb, ruleSource(c.entity.Access, verb), which),
			Fix: "only records the rule allows can be changed this way; describe the entity type for its rules"}}
	}
	if c.record != nil && !records.Allows(rule, c.record, c.Principal) {
		return refused("record")
	}
	if verb == "delete" {
		return nil
	}
	after := maps.Clone(c.record)
	for _, ev := range events {
		data, _ := json.Marshal(ev.Data)
		next, err := Apply(after, live.Event{Type: ev.Type, Target: c.Target, Data: data, UserID: c.Principal.UserID, TenantID: c.Principal.TenantID, Time: time.Now()})
		if err != nil {
			return problem("", err.Error(), "")
		}
		after = next
	}
	if after != nil && !records.Allows(rule, after, c.Principal) {
		return refused("change")
	}
	return nil
}

func ruleSource(a records.Access, verb string) string {
	return map[string]string{"read": a.Read, "create": a.Create, "update": a.Update, "delete": a.Delete}[verb]
}

func decode[T any](raw json.RawMessage) (T, []live.Problem) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, problem("args", err.Error(), "")
	}
	return v, nil
}

type spec struct {
	id, title, description, args, example, action string
	events                                        []string
	create, internal                              bool
	decide                                        func(c *decision) ([]live.NewEvent, []live.Problem)
}

const entityArg = `"entity":{"type":"string","description":"The entity type's id"}`
const expectArg = `"expect":{"type":"integer","description":"The record's _seq as last shown; refused with record_changed if it moved since"}`

// Contribute registers the records' projector, commands and queries; deps
// returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: "records"}, Title: "Records",
		Description: "Every record of every entity type, keyed <entityId>.<recordId>: its values by field id, and _id, _entity, _seq, _createdAt, _updatedAt, _createdBy, _updatedBy and _problems",
		Area:        live.AreaRecords, Fold: fold,
		Keep: func(k live.Keep) ([]string, error) { return keep(deps(), k) },
	})
	for _, s := range commandSpecs() {
		s := s
		cmd := live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "records"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events,
			Area: live.AreaRecords, TargetType: "record", Projection: Projector, Action: s.action, Internal: s.internal,
			TypeOf: func(args json.RawMessage) (string, *live.Problem) {
				var a struct{ Entity string }
				_ = json.Unmarshal(args, &a)
				if !live.ValidToken(a.Entity) {
					return "", &live.Problem{Path: "args.entity", Message: fmt.Sprintf("%q is not an entity type id", a.Entity), Fix: "list the entity types with entity.list"}
				}
				return a.Entity, nil
			},
			Resource: func(p features.Principal, t live.Target) string { return records.Resource(p, t.Type) },
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				c := &decision{Decision: d, deps: deps()}
				if err := Follow(d.ServiceArgs, c.deps); err != nil {
					log.Error(d.Context(), err, "the converter cannot start")
				}
				if d.State != nil {
					if err := json.Unmarshal(d.State, &c.record); err != nil {
						return nil, problem("", "the record cannot be read: "+err.Error(), "")
					}
				}
				if c.record == nil && !s.create {
					return nil, problem("target", fmt.Sprintf("there is no %s record %q", d.Target.Type, d.Target.ID), "list them with records.query")
				}
				if s.internal {
					return s.decide(c)
				}
				e, problems := Entity(d.ServiceArgs, c.deps.Projections, d.Target.Type)
				if problems != nil {
					return nil, problems
				}
				c.entity = e
				// A record the read rule hides is not there for the principal
				if c.record != nil && !records.Allows(e.Rule("read"), c.record, d.Principal) {
					return nil, problem("target", fmt.Sprintf("there is no %s record %q", d.Target.Type, d.Target.ID), "list them with records.query")
				}
				events, problems := s.decide(c)
				if problems != nil || len(events) == 0 {
					return events, problems
				}
				return events, c.rules(s.action, events)
			},
		}
		if s.create {
			cmd.NewTarget = func() string { return strconv.FormatUint(features.NewTSID(), 10) }
		}
		live.Commands.Register(reg, cmd)
	}
	contributeQueries(reg, deps)
}

func commandSpecs() []spec {
	return []spec{
		{
			id: "record.create", title: "Create a record", action: "record.create", create: true, events: []string{EventCreated},
			description: "Creates a record of an entity type with its values by field id. Fields left out get their default; required fields need a value. The target is the new record's id: leave it out to get one.",
			args:        `{"type":"object","properties":{` + entityArg + `,"values":{"type":"object","description":"Field id → value"}},"required":["entity"]}`,
			example:     `{"entity":"task","values":{"title":"Write the plan","done":false,"due":"2026-11-15"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if c.record != nil {
					return nil, problem("target", fmt.Sprintf("there is already a record %q", c.Target.ID), "leave the target out to get a new id")
				}
				a, problems := decode[struct {
					Values map[string]json.RawMessage
				}](c.Args)
				if problems != nil {
					return nil, problems
				}
				values := map[string]json.RawMessage{}
				for id, v := range a.Values {
					out, ps := c.value(id, v, "args.values."+id)
					problems = append(problems, ps...)
					if out != nil {
						values[id] = out
					}
				}
				for _, f := range c.entity.Visible() {
					if _, set := values[f.ID]; set {
						continue
					}
					if t, _ := records.FieldTypeOf(f.Type); t.Computed {
						continue
					}
					if !records.IsNull(f.Default) {
						if out, err := records.CheckValue(f, f.Default); err == nil && out != nil {
							values[f.ID] = out
							continue
						}
					}
					if f.Required {
						if _, sent := a.Values[f.ID]; !sent {
							problems = append(problems, live.Problem{Path: "args.values." + f.ID, Message: f.Name + " is required", Fix: "give it a value"})
						}
					}
				}
				if problems != nil {
					return nil, problems
				}
				return []live.NewEvent{{Type: EventCreated, Data: Change{Values: values}}}, nil
			},
		},
		{
			id: "record.update", title: "Change a record", action: "record.update", events: []string{EventUpdated},
			description: "Sets some of a record's values by field id; null clears one. Only the fields that change are recorded, so two people changing different fields both keep their change. expect insists on the version shown (its _seq).",
			args:        `{"type":"object","properties":{` + entityArg + `,"set":{"type":"object","description":"Field id → new value, null to clear"},` + expectArg + `},"required":["entity","set"]}`,
			example:     `{"entity":"task","set":{"done":true}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := decode[struct {
					Set    map[string]json.RawMessage
					Expect uint64
				}](c.Args)
				if problems != nil {
					return nil, problems
				}
				if problems := c.expect(struct{ Expect uint64 }{a.Expect}); problems != nil {
					return nil, problems
				}
				change := Change{Values: map[string]json.RawMessage{}, Old: map[string]json.RawMessage{}}
				for id, v := range a.Set {
					out, ps := c.value(id, v, "args.set."+id)
					problems = append(problems, ps...)
					if ps == nil && !same(out, c.record[id]) {
						change.Values[id] = orNull(out)
						change.Old[id] = orNull(c.record[id])
					}
				}
				if problems != nil || len(change.Values) == 0 {
					return nil, problems
				}
				return []live.NewEvent{{Type: EventUpdated, Data: change}}, nil
			},
		},
		{
			id: "record.delete", title: "Delete a record", action: "record.delete", events: []string{EventDeleted},
			description: "Deletes a record. Its events keep its history.",
			args:        `{"type":"object","properties":{` + entityArg + `,` + expectArg + `},"required":["entity"]}`,
			example:     `{"entity":"task"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := decode[struct{ Expect uint64 }](c.Args)
				if problems == nil {
					problems = c.expect(a)
				}
				if problems != nil {
					return nil, problems
				}
				return []live.NewEvent{{Type: EventDeleted, Data: Change{Old: c.record.Values()}}}, nil
			},
		},
		listSpec("record.addTo", "Add to a list", EventAdded,
			"Adds items to a multiEnum, tags, list or to-many relation field, keeping what is there: two people adding at once both keep theirs. Items already there are left."),
		listSpec("record.removeFrom", "Remove from a list", EventRemoved,
			"Removes items from a multiEnum, tags, list or to-many relation field, leaving the others."),
		{
			id: "record.convert", title: "Convert a record's value", action: "record.update", internal: true, events: []string{EventConverted},
			description: "Converts one field's value when its type changes; sent by the converter.",
			args:        `{"type":"object","properties":{` + entityArg + `,"field":{"type":"string"},"from":{"type":"object"},"to":{"type":"object"}},"required":["entity","field","from","to"]}`,
			example:     `{"entity":"task","field":"size","from":{"id":"size","name":"size","type":"text"},"to":{"id":"size","name":"size","type":"number"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := decode[struct {
					Field    string
					From, To records.Field
				}](c.Args)
				if problems != nil {
					return nil, problems
				}
				v := c.record[a.Field]
				if records.IsNull(v) {
					return nil, nil
				}
				out, err := records.Convert(a.From, a.To, v)
				if err == nil && same(out, v) {
					return nil, nil
				}
				change := Change{Values: map[string]json.RawMessage{a.Field: orNull(out)}, Old: map[string]json.RawMessage{a.Field: v}}
				if err != nil {
					change.Problems = map[string]string{a.Field: fmt.Sprintf("%s could not become a %s (%v), so it was cleared; its old value is in the record's history", v, a.To.Type, err)}
				}
				return []live.NewEvent{{Type: EventConverted, Data: change}}, nil
			},
		},
	}
}

func listSpec(id, title, event, description string) spec {
	return spec{
		id: id, title: title, action: "record.update", events: []string{event}, description: description,
		args:    `{"type":"object","properties":{` + entityArg + `,"field":{"type":"string","description":"The field's id"},"items":{"type":"array","description":"The items, as the field holds them"},` + expectArg + `},"required":["entity","field","items"]}`,
		example: `{"entity":"task","field":"tags","items":["urgent"]}`,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := decode[struct {
				Field  string
				Items  json.RawMessage
				Expect uint64
			}](c.Args)
			if problems == nil {
				problems = c.expect(struct{ Expect uint64 }{a.Expect})
			}
			if problems != nil {
				return nil, problems
			}
			f, problems := c.field(a.Field, "args.field")
			if problems != nil {
				return nil, problems
			}
			if _, many := records.ItemOf(f); !many {
				return nil, problem("args.field", fmt.Sprintf("%s is a %s, not a list", f.Name, f.Type), "set it with record.update")
			}
			out, err := records.CheckValue(f, a.Items)
			if err != nil {
				return nil, problem("args.items", f.Name+": "+err.Error(), "")
			}
			var items, have []json.RawMessage
			_ = json.Unmarshal(out, &items)
			_ = json.Unmarshal(c.record[f.ID], &have)
			var changed []json.RawMessage
			for _, it := range items {
				there := false
				for _, h := range have {
					there = there || same(h, it)
				}
				if there == (event == EventRemoved) {
					changed = append(changed, it)
				}
			}
			if len(changed) == 0 {
				return nil, nil
			}
			return []live.NewEvent{{Type: event, Data: Change{Field: f.ID, Items: changed}}}, nil
		},
	}
}

func orNull(v json.RawMessage) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	return v
}

// waitTimeout is how long a caller waits for its own commit to be
// projected.
const waitTimeout = 2 * time.Second
