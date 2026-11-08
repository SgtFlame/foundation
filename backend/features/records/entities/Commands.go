package entities

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"gorm.io/gorm"
)

var log = records.Log.Tags("entities")

// Projections are what the entity types read: the projections, waiting
// for a commit to be projected, and following the def stream
// (live/project.Projections).
type Projections interface {
	live.Store
	WaitFor(ctx context.Context, tenant uint64, projector, key string, seq uint64, timeout time.Duration) (live.Projected, error)
	Follow(ctx context.Context, tenant uint64, area live.Area, name string, subjects []string, fn func(live.Event) error) error
}

// Deps are what the commands, queries and versions follower need, made
// after the registry.
type Deps struct {
	Projections Projections
	DB          *gorm.DB // the tenant database, which holds the published versions
	// OnPublish, when set, starts the other followers of published
	// versions before one commits: the records' converter, which converts
	// values when a field's type changes.
	OnPublish func(args features.ServiceArgs) error
}

// decision is one command's view of its entity type.
type decision struct {
	live.Decision
	deps   Deps
	draft  Draft
	exists bool
}

func (c *decision) entity() *records.EntityType { return &c.draft.Entity }

// known reports whether an entity type exists in the tenant.
func (c *decision) known(id string) bool {
	if !live.ValidToken(id) {
		return false
	}
	_, ok, err := c.deps.Projections.Get(c.Context(), c.Principal.TenantID, DraftProjector, TargetType+"."+id)
	return err == nil && ok
}

func problem(path, message, fix string) []live.Problem {
	return []live.Problem{{Path: path, Message: message, Fix: fix}}
}

func ev(typ string, data any) []live.NewEvent { return []live.NewEvent{{Type: typ, Data: data}} }

func args[T any](c *decision) (T, []live.Problem) {
	v, err := decode[T](c.Args)
	if err != nil {
		return v, problem("args", err.Error(), "")
	}
	return v, nil
}

// slug is a field id made from its name: "Due date" is due_date.
func slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "_"):
			b.WriteRune('_')
		}
	}
	s := strings.TrimSuffix(b.String(), "_")
	if s == "" {
		return "field"
	}
	return s
}

// newID is an id from base that taken does not hold.
func newID(base string, taken func(string) bool) string {
	id := base
	for n := 2; taken(id); n++ {
		id = base + "_" + strconv.Itoa(n)
	}
	return id
}

// prepare gives a new field its id and completes it.
func (c *decision) prepare(f *records.Field, path string) []live.Problem {
	if f.ID == "" {
		f.ID = newID(slug(f.Name), func(id string) bool { _, _, ok := c.entity().FieldByID(id); return ok })
	} else if !live.ValidToken(f.ID) {
		return problem(path+".id", fmt.Sprintf("%q is not a valid field id", f.ID), "use letters, digits, '-' and '_', or leave it out to get one from the name")
	} else if _, _, dup := c.entity().FieldByID(f.ID); dup {
		return problem(path+".id", fmt.Sprintf("there is already a field %q", f.ID), "leave the id out to get a new one")
	}
	f.Hidden = false
	records.Normalize(f)
	return nil
}

// field is a field of the entity type by id, for the commands that name one.
func (c *decision) field(id string) (records.Field, int, []live.Problem) {
	f, i, ok := c.entity().FieldByID(id)
	if !ok {
		return f, i, problem("args.field", fmt.Sprintf("there is no field %q", id), "describe the entity type with entity.describe for its fields' ids")
	}
	return f, i, nil
}

const optionSchema = `{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"color":{"type":"string"}},"required":["id","name"]}`

// fieldProps are a field's properties, as entity.addField and
// entity.updateField take them.
const fieldProps = `"name":{"type":"string"},"type":{"type":"string","description":"A field type (entity.listFieldTypes)"},"description":{"type":"string"},"required":{"type":"boolean"},"default":{},` +
	`"options":{"type":"array","items":` + optionSchema + `,"description":"enum, multiEnum"},` +
	`"relation":{"type":"object","properties":{"entity":{"type":"string"},"many":{"type":"boolean"},"inverse":{"type":"string"}},"required":["entity"],"description":"relation: the entity type it points to; \"user\" is the tenant's users"},` +
	`"scale":{"type":"integer","description":"decimal, money"},"currency":{"type":"string","description":"money"},` +
	`"items":{"type":"object","description":"list: what each item is, {type, …}"},"fields":{"type":"array","items":{"type":"object"},"description":"object: nested fields"},` +
	`"schema":{"type":"object","description":"json"},"formula":{"type":"string"},` +
	`"rollup":{"type":"object","properties":{"relation":{"type":"string"},"field":{"type":"string"},"aggregate":{"type":"string"}},"required":["relation","aggregate"]}`

const fieldSchema = `{"type":"object","properties":{"id":{"type":"string","description":"Its id; made from the name when left out"},` + fieldProps + `},"required":["name","type"]}`

type spec struct {
	id, title, description, args, example string
	events                                []string
	action                                string
	create                                bool // may target an entity type that does not exist yet
	decide                                func(c *decision) ([]live.NewEvent, []live.Problem)
}

// Contribute registers the entity types' projectors, commands and
// queries; deps returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: DraftProjector, Source: "records"}, Title: "Entity type drafts",
		Description: "Each entity type's draft, as its entity.* commands left it: {entity, rev, published, publishedRev, updatedBy, updatedAt}",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: fold,
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: PublishedProjector, Source: "records"}, Title: "Published entity types",
		Description: "Each entity type's last published version, the one records follow",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: foldPublished,
	})

	for _, s := range commandSpecs() {
		s := s
		action := s.action
		if action == "" {
			action = "records.write"
		}
		live.Commands.Register(reg, live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "records"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events,
			Area: live.AreaDefinitions, TargetType: TargetType, Projection: DraftProjector, Action: action,
			Resource: func(p features.Principal, t live.Target) string { return records.Resource(p, t.ID) },
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				c := &decision{Decision: d, deps: deps()}
				if d.State != nil {
					if err := json.Unmarshal(d.State, &c.draft); err != nil {
						return nil, problem("", "the draft cannot be read: "+err.Error(), "")
					}
					c.exists = true
				}
				if !c.exists && !s.create {
					return nil, problem("target", fmt.Sprintf("there is no entity type %q", d.Target.ID), "make it first with entity.create {name, fields}")
				}
				events, problems := s.decide(c)
				if problems != nil || len(events) == 0 {
					return nil, problems
				}
				if problems := c.check(events); len(problems) > 0 {
					return nil, problems
				}
				c.undo(s.id, events)
				return events, nil
			},
			Report: func(args features.ServiceArgs, t live.Target, seq uint64) any { return report(args, deps(), t.ID, seq) },
		})
	}
	contributeQueries(reg, deps)
}

// check applies the events to a copy of the draft and checks the result,
// so no command leaves an invalid definition.
func (c *decision) check(events []live.NewEvent) []live.Problem {
	raw, _ := json.Marshal(c.draft)
	var d Draft
	_ = json.Unmarshal(raw, &d)
	for _, e := range events {
		data, err := json.Marshal(e.Data)
		if err == nil {
			err = Apply(&d, live.Event{Type: e.Type, Target: c.Target, Data: data})
		}
		if err != nil {
			return problem("", err.Error(), "")
		}
	}
	problems := records.Check(d.Entity, c.known)
	for i := range problems {
		problems[i].Path = strings.TrimSuffix("entity."+problems[i].Path, ".")
	}
	return problems
}

// report is an entity type's rev once a commit at seq is projected, for
// the commands' tools.
func report(args features.ServiceArgs, deps Deps, id string, seq uint64) any {
	v, err := deps.Projections.WaitFor(args.Context(), args.Principal.TenantID, DraftProjector, TargetType+"."+id, seq, 2*time.Second)
	if err != nil || v.Value == nil {
		return nil
	}
	var d Draft
	if json.Unmarshal(v.Value, &d) != nil {
		return nil
	}
	return map[string]any{"rev": d.Rev, "published": d.Published, "fields": len(d.Entity.Visible())}
}

func commandSpecs() []spec {
	return []spec{
		{
			id: "entity.create", title: "Create an entity type", create: true, events: []string{EventCreated},
			description: "Makes a new entity type (a schema for records, such as Task) with the target as its id, and its fields. A field's id is made from its name when left out; records keep values by field id, so a rename never touches them. The title field is the first text field unless given.",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string","description":"What a record of it is, for a model"},"owner":{"type":"string","description":"app:<appId>, or tenant for a type apps share"},"titleField":{"type":"string"},"fields":{"type":"array","items":` + fieldSchema + `}},"required":["name"]}`,
			example:     `{"name":"Task","owner":"app:hello","fields":[{"name":"title","type":"text","required":true},{"name":"done","type":"bool"},{"name":"due","type":"date"},{"name":"assignee","type":"relation","relation":{"entity":"user"}}]}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if c.exists {
					return nil, problem("target", fmt.Sprintf("there is already an entity type %q", c.Target.ID), "choose another id, or change it with the other entity.* commands")
				}
				if c.Target.ID == records.UserEntity {
					return nil, problem("target", `"user" is the built-in entity type of the tenant's users`, "choose another id")
				}
				if strings.HasPrefix(c.Target.ID, "_") {
					return nil, problem("target", fmt.Sprintf("%q starts with '_', which the records keep for their own data (such as autoNumber counters)", c.Target.ID), "choose an id starting with a letter or digit")
				}
				a, problems := args[struct {
					Name, Description, Owner, TitleField string
					Fields                               []records.Field
				}](c)
				if problems != nil {
					return nil, problems
				}
				e := records.EntityType{ID: c.Target.ID, Name: a.Name, Description: a.Description, Owner: a.Owner, TitleField: a.TitleField,
					Fields: []records.Field{}, Indexes: []records.Index{}}
				for i, f := range a.Fields {
					if problems := c.prepare(&f, fmt.Sprintf("args.fields.%d", i)); problems != nil {
						return nil, problems
					}
					e.Fields = append(e.Fields, f)
					c.draft.Entity.Fields = e.Fields // so the next field's id is new
				}
				if e.TitleField == "" {
					if i := slices.IndexFunc(e.Fields, func(f records.Field) bool { return f.Type == "text" }); i >= 0 {
						e.TitleField = e.Fields[i].ID
					}
				}
				c.draft.Entity = records.EntityType{}
				return ev(EventCreated, map[string]any{"entity": e}), nil
			},
		},
		{
			id: "entity.import", title: "Import an entity type", create: true, events: []string{EventImported},
			description: "Replaces the target entity type's whole draft with a definition given as JSON (its name, description, owner, title field, fields with their ids, indexes and access, as entity.describe gives it), or makes it. Records keep their values by field id, so a field kept by id keeps its values. Its published versions stay until it is published again. Importing an app (app.import) uses it.",
			args:        `{"type":"object","properties":{"entity":{"type":"object","description":"An entity type as entity.describe returns it: {name, description?, owner?, titleField?, fields, indexes?, access?}"}},"required":["entity"]}`,
			example:     `{"entity":{"name":"Task","owner":"app:tasks","titleField":"title","fields":[{"id":"title","name":"title","type":"text","required":true},{"id":"done","name":"done","type":"bool"}]}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if c.Target.ID == records.UserEntity || strings.HasPrefix(c.Target.ID, "_") {
					return nil, problem("target", fmt.Sprintf("%q cannot be an entity type's id", c.Target.ID), "choose an id starting with a letter or digit, other than user")
				}
				a, problems := args[struct{ Entity *records.EntityType }](c)
				if problems != nil {
					return nil, problems
				}
				if a.Entity == nil {
					return nil, problem("args.entity", "the definition is missing", "give the entity type as entity.describe returns it")
				}
				e := *a.Entity
				e.ID, e.Version, e.Hidden = c.Target.ID, 0, false
				if e.Fields == nil {
					e.Fields = []records.Field{}
				}
				if e.Indexes == nil {
					e.Indexes = []records.Index{}
				}
				for i := range e.Fields {
					if !live.ValidToken(e.Fields[i].ID) {
						return nil, problem(fmt.Sprintf("args.entity.fields.%d.id", i), fmt.Sprintf("%q is not a valid field id", e.Fields[i].ID), "give every field its id, as entity.describe does")
					}
					records.Normalize(&e.Fields[i])
				}
				if c.exists {
					cur := *c.entity()
					cur.Version = 0
					if equalJSON(cur, e) {
						return nil, nil
					}
				}
				return ev(EventImported, map[string]any{"entity": e}), nil
			},
		},
		{
			id: "entity.setInfo", title: "Describe an entity type", events: []string{EventInfoSet},
			description: "Sets an entity type's name, description, owner and title field (the field shown as a record's title).",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"owner":{"type":"string"},"titleField":{"type":"string","description":"A field's id"}}}`,
			example:     `{"description":"Something to do, with a due date and an assignee"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				in, problems := args[Info](c)
				if problems != nil {
					return nil, problems
				}
				if in == (Info{}) {
					return nil, nil
				}
				return ev(EventInfoSet, in), nil
			},
		},
		{
			id: "entity.addField", title: "Add a field", events: []string{EventFieldAdded, EventInfoSet},
			description: "Adds a field to an entity type: its name, a type (entity.listFieldTypes) and what that type needs, e.g. options for an enum or relation {entity} for a relation. Its id is made from its name when left out. The first text field becomes the title field.",
			args:        fieldSchema,
			example:     `{"name":"priority","type":"enum","options":[{"id":"low","name":"Low"},{"id":"medium","name":"Medium"},{"id":"high","name":"High"}],"default":"medium"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				f, problems := args[records.Field](c)
				if problems != nil {
					return nil, problems
				}
				if problems := c.prepare(&f, "args"); problems != nil {
					return nil, problems
				}
				events := ev(EventFieldAdded, map[string]any{"field": f})
				if c.entity().TitleField == "" && f.Type == "text" {
					events = append(events, live.NewEvent{Type: EventInfoSet, Data: Info{TitleField: &f.ID}})
				}
				return events, nil
			},
		},
		{
			id: "entity.updateField", title: "Change a field", events: []string{EventFieldUpdated},
			description: "Changes a field by its id: its name (a rename keeps the records' values, which are kept by id), type, description, required, default, options and the rest of what its type needs. A property left out stays; null clears it. A new type drops what the old one needed, and when the change is published the records' values are converted; values that cannot be converted are cleared and listed as problems on their records. A computed field's type cannot change.",
			args:        `{"type":"object","properties":{"field":{"type":"string","description":"The field's id"},` + fieldProps + `},"required":["field"]}`,
			example:     `{"field":"title","name":"summary"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				var patch map[string]json.RawMessage
				if err := json.Unmarshal(c.Args, &patch); err != nil {
					return nil, problem("args", err.Error(), "")
				}
				var id string
				_ = json.Unmarshal(patch["field"], &id)
				old, _, problems := c.field(id)
				if problems != nil {
					return nil, problems
				}
				raw, _ := json.Marshal(old)
				var merged map[string]json.RawMessage
				_ = json.Unmarshal(raw, &merged)
				var newType string
				if _ = json.Unmarshal(patch["type"], &newType); newType != "" && newType != old.Type {
					// What the old type needed goes, unless the patch gives it again
					for _, k := range []string{"options", "relation", "scale", "currency", "items", "fields", "schema", "formula", "rollup", "default"} {
						delete(merged, k)
					}
				}
				for k, v := range patch {
					switch {
					case k == "field" || k == "id" || k == "hidden":
					case string(v) == "null":
						delete(merged, k)
					default:
						merged[k] = v
					}
				}
				raw, _ = json.Marshal(merged)
				f, err := decode[records.Field](raw)
				if err != nil {
					return nil, problem("args", err.Error(), "")
				}
				if f.Type != old.Type && (computed(f.Type) || computed(old.Type)) {
					return nil, problem("args.type", fmt.Sprintf("field %s cannot change from %s to %s: computed fields keep no values to convert", id, old.Type, f.Type), "add a new field of type "+f.Type+" and remove this one")
				}
				records.Normalize(&f)
				if raw, _ := json.Marshal(f); string(raw) == string(must(json.Marshal(old))) {
					return nil, nil
				}
				return ev(EventFieldUpdated, map[string]any{"field": f}), nil
			},
		},
		{
			id: "entity.removeField", title: "Remove a field", events: []string{EventFieldRemoved},
			description: "Removes a field from an entity type. It is hidden, not deleted: records keep its values until a purge, and entity.restoreField brings it back with them. The title field, and a field an index or rollup uses, cannot be removed until they no longer use it.",
			args:        `{"type":"object","properties":{"field":{"type":"string","description":"The field's id"}},"required":["field"]}`,
			example:     `{"field":"due"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Field string }](c)
				if problems != nil {
					return nil, problems
				}
				f, _, problems := c.field(a.Field)
				if problems != nil || f.Hidden {
					return nil, problems
				}
				if c.entity().TitleField == f.ID {
					return nil, problem("args.field", fmt.Sprintf("%s is the title field", f.ID), "set another titleField with entity.setInfo first")
				}
				return ev(EventFieldRemoved, map[string]any{"field": f.ID}), nil
			},
		},
		{
			id: "entity.restoreField", title: "Restore a field", events: []string{EventFieldRestored},
			description: "Brings back a removed field, with the values records kept for it.",
			args:        `{"type":"object","properties":{"field":{"type":"string","description":"The field's id"}},"required":["field"]}`,
			example:     `{"field":"due"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Field string }](c)
				if problems != nil {
					return nil, problems
				}
				f, _, problems := c.field(a.Field)
				if problems != nil || !f.Hidden {
					return nil, problems
				}
				return ev(EventFieldRestored, map[string]any{"field": f.ID}), nil
			},
		},
		{
			id: "entity.addIndex", title: "Add an index", events: []string{EventIndexAdded},
			description: "Declares an index over fields, in order, so queries that filter or sort by them are fast; unique makes no two records share its values. Its id is by_<fields> when left out.",
			args:        `{"type":"object","properties":{"id":{"type":"string"},"fields":{"type":"array","items":{"type":"string"},"description":"Field ids, in order"},"unique":{"type":"boolean"}},"required":["fields"]}`,
			example:     `{"fields":["done","due"]}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				x, problems := args[records.Index](c)
				if problems != nil {
					return nil, problems
				}
				if x.ID == "" {
					x.ID = newID(slug("by_"+strings.Join(x.Fields, "_")), func(id string) bool {
						return slices.ContainsFunc(c.entity().Indexes, func(i records.Index) bool { return i.ID == id })
					})
				}
				return ev(EventIndexAdded, map[string]any{"index": x}), nil
			},
		},
		{
			id: "entity.removeIndex", title: "Remove an index", events: []string{EventIndexRemoved},
			description: "Removes a declared index.",
			args:        `{"type":"object","properties":{"index":{"type":"string","description":"The index's id"}},"required":["index"]}`,
			example:     `{"index":"by_done_due"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Index string }](c)
				if problems != nil {
					return nil, problems
				}
				if !slices.ContainsFunc(c.entity().Indexes, func(i records.Index) bool { return i.ID == a.Index }) {
					return nil, nil
				}
				return ev(EventIndexRemoved, map[string]any{"index": a.Index}), nil
			},
		},
		{
			id: "entity.setAccess", title: "Set an entity type's row rules", events: []string{EventAccessSet},
			description: "Sets the row rules of an entity type: an expression over record and principal per action (read, create, update, delete), such as record.assignee == principal.user; empty for no rule. They replace the rules there were.",
			args:        `{"type":"object","properties":{"access":{"type":"object","properties":{"read":{"type":"string"},"create":{"type":"string"},"update":{"type":"string"},"delete":{"type":"string"}}}},"required":["access"]}`,
			example:     `{"access":{"update":"record.assignee == principal.user"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Access records.Access }](c)
				if problems != nil {
					return nil, problems
				}
				if a.Access == c.entity().Access {
					return nil, nil
				}
				return ev(EventAccessSet, map[string]any{"access": a.Access}), nil
			},
		},
		{
			id: "entity.publish", title: "Publish an entity type", events: []string{EventPublished}, action: "records.publish",
			description: "Publishes the draft as a new version, which records follow from then on; it is also kept in the tenant database. rev is the draft's rev the publisher last saw: a change made since is not published unseen.",
			args:        `{"type":"object","properties":{"rev":{"type":"integer","description":"The draft's rev as last seen (entity.* results and entity.describe give it)"}},"required":["rev"]}`,
			example:     `{"rev":5}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Rev int }](c)
				if problems != nil {
					return nil, problems
				}
				if a.Rev != c.draft.Rev {
					return nil, problem("args.rev", fmt.Sprintf("the draft changed since rev %d: it is at rev %d", a.Rev, c.draft.Rev), "look at the changes, then publish rev "+strconv.Itoa(c.draft.Rev))
				}
				// The copy into the tenant database runs before the event lands
				if err := Follow(c.ServiceArgs, c.deps); err != nil {
					return nil, problem("", "the published versions cannot be kept: "+err.Error(), "")
				}
				if c.deps.OnPublish != nil {
					if err := c.deps.OnPublish(c.ServiceArgs); err != nil {
						return nil, problem("", "the records cannot follow the new version: "+err.Error(), "")
					}
				}
				e := *c.entity()
				e.Version = c.draft.Published + 1
				e.Fields = slices.Clone(e.Fields)
				if problems := records.Resolve(&e, c.published); problems != nil {
					for i := range problems {
						problems[i].Path = "entity." + problems[i].Path
					}
					return nil, problems
				}
				prev := c.lastPublished()
				return ev(EventPublished, Publication{Version: e.Version, Entity: e, TypeChanges: typeChanges(prev, e), IndexChanges: indexChanges(prev, e),
					ComputedChanges: computedChanges(prev, e), SearchChanged: searchChanged(prev, e)}), nil
			},
		},
		{
			id: "entity.delete", title: "Delete an entity type", events: []string{EventDeleted},
			description: "Deletes an entity type at once, without publishing. It is hidden, not erased: its records are kept, record commands on it are refused, and entity.restore brings it back with them.",
			args:        `{"type":"object","properties":{}}`,
			example:     `{}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if c.entity().Hidden {
					return nil, nil
				}
				return ev(EventDeleted, map[string]any{}), nil
			},
		},
		{
			id: "entity.restore", title: "Restore an entity type", events: []string{EventRestored},
			description: "Brings back a deleted entity type with its records.",
			args:        `{"type":"object","properties":{}}`,
			example:     `{}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if !c.entity().Hidden {
					return nil, nil
				}
				return ev(EventRestored, map[string]any{}), nil
			},
		},
	}
}

// lastPublished is the target's last published version; nil before the
// first.
// published is another entity type's published version, for the
// rollups that read it.
func (c *decision) published(id string) (records.EntityType, bool) {
	var e records.EntityType
	v, ok, err := c.deps.Projections.Get(c.Context(), c.Principal.TenantID, PublishedProjector, TargetType+"."+id)
	if err != nil || !ok || json.Unmarshal(v.Value, &e) != nil {
		return e, false
	}
	return e, true
}

// computedChanges are e's computed fields that are new or differ from
// prev's: the records' recompute writes their values again.
func computedChanges(prev *records.EntityType, e records.EntityType) []string {
	var out []string
	for _, f := range e.Visible() {
		if !records.IsComputed(f) {
			continue
		}
		if prev != nil {
			if old, _, ok := prev.FieldByID(f.ID); ok && !old.Hidden {
				a, _ := json.Marshal([]any{old.Type, old.Formula, old.Rollup, old.Returns, old.Scale})
				b, _ := json.Marshal([]any{f.Type, f.Formula, f.Rollup, f.Returns, f.Scale})
				if string(a) == string(b) {
					continue
				}
			}
		}
		out = append(out, f.ID)
	}
	return out
}

// searchChanged reports whether the fields e's records are searched by
// differ from prev's, so the search index is rebuilt for them.
func searchChanged(prev *records.EntityType, e records.EntityType) bool {
	sig := func(e *records.EntityType) string {
		if e == nil {
			return ""
		}
		var ids []string
		for _, f := range e.Visible() {
			if f.Searchable {
				ids = append(ids, f.ID+":"+f.Type)
			}
		}
		return strings.Join(ids, ",") + "|" + e.TitleField
	}
	return sig(prev) != sig(&e)
}

func (c *decision) lastPublished() *records.EntityType {
	v, ok, err := c.deps.Projections.Get(c.Context(), c.Principal.TenantID, PublishedProjector, c.Target.Key())
	if err != nil || !ok {
		return nil
	}
	var prev records.EntityType
	if json.Unmarshal(v.Value, &prev) != nil {
		return nil
	}
	return &prev
}

// typeChanges are the fields of e whose type differs from prev's.
func typeChanges(prev *records.EntityType, e records.EntityType) []TypeChange {
	if prev == nil {
		return nil
	}
	var out []TypeChange
	for _, f := range e.Fields {
		if old, _, ok := prev.FieldByID(f.ID); ok && old.Type != f.Type {
			out = append(out, TypeChange{Field: f.ID, From: old, To: f})
		}
	}
	return out
}

// indexChanges are the indexes of e or prev that differ between them: in
// their fields, or in how those fields sort (type, options, scale,
// currency, relation).
func indexChanges(prev *records.EntityType, e records.EntityType) []string {
	sig := func(e *records.EntityType) map[string]string {
		out := map[string]string{}
		if e == nil {
			return out
		}
		for _, x := range e.Indexes {
			s := fmt.Sprint(x.Fields, x.Unique)
			for _, id := range x.Fields {
				f, _, _ := e.FieldByID(id)
				raw, _ := json.Marshal([]any{f.Type, f.Returns, f.Options, f.Scale, f.Currency, f.Relation, f.Items})
				s += string(raw)
			}
			out[x.ID] = s
		}
		return out
	}
	before, after := sig(prev), sig(&e)
	var out []string
	for id, s := range after {
		if before[id] != s {
			out = append(out, id)
		}
	}
	for id := range before {
		if _, ok := after[id]; !ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func computed(typ string) bool {
	t, _ := records.FieldTypeOf(typ)
	return t.Computed
}

func must(raw []byte, _ error) []byte { return raw }

// equalJSON compares two values as their JSON.
func equalJSON(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}
