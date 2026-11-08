package manifests

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"gorm.io/gorm"
)

var log = apps.Log.Tags("manifests")

// Projections are what manifests read: the projections, waiting for a
// commit to be projected, and following the def stream
// (live/project.Projections).
type Projections interface {
	live.Store
	WaitFor(ctx context.Context, tenant uint64, projector, key string, seq uint64, timeout time.Duration) (live.Projected, error)
	Follow(ctx context.Context, tenant uint64, area live.Area, name string, subjects []string, fn func(live.Event) error) error
	History(ctx context.Context, tenant uint64, t live.Target, fn func(live.Event) bool) error
}

// Deps are what the commands, queries and follower need, made after the
// registry.
type Deps struct {
	Projections Projections
	DB          *gorm.DB // the tenant database, which holds the apps
}

// decision is one command's view of its app.
type decision struct {
	live.Decision
	deps   Deps
	reg    *extensions.Registry
	draft  Draft
	exists bool
}

func (c *decision) manifest() *apps.Manifest { return &c.draft.Manifest }

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

// get reads a projected value of the principal's tenant.
func get(ctx context.Context, store live.Store, tenant uint64, projector, key string) (json.RawMessage, bool) {
	v, ok, err := store.Get(ctx, tenant, projector, key)
	if err != nil || !ok || v.Value == nil {
		return nil, false
	}
	return v.Value, true
}

// Env is what an app of the principal's tenant is checked against: its
// prefabs, graphs and entity types, drafted or published, and the
// primitives.
func Env(ctx context.Context, store live.Store, tenant uint64, reg *extensions.Registry) apps.Env {
	return envFrom(func(projector, key string) (json.RawMessage, bool) { return get(ctx, store, tenant, projector, key) }, reg)
}

// envFrom is an Env reading the projections through get.
func envFrom(get func(projector, key string) (json.RawMessage, bool), reg *extensions.Registry) apps.Env {
	read := func(projector, key string, v any) bool {
		raw, ok := get(projector, key)
		return ok && json.Unmarshal(raw, v) == nil
	}
	return apps.Env{
		Catalog: ui.Catalog(reg),
		Prefab: func(id string) (ui.Prefab, bool) {
			var d prefabs.Draft
			if read(prefabs.DraftProjector, prefabs.TargetType+"."+id, &d) {
				return d.Prefab, true
			}
			var p ui.Prefab
			return p, read(prefabs.PublishedProjector, prefabs.TargetType+"."+id, &p)
		},
		Graph: func(id string) (cognition.Graph, bool) {
			var d drafts.Draft
			if read(drafts.DraftProjector, drafts.TargetType+"."+id, &d) {
				return d.Graph, true
			}
			var g cognition.Graph
			if read(drafts.PublishedProjector, drafts.TargetType+"."+id, &g) {
				return g, true
			}
			built := cognition.Graphs.All(reg)
			if i := slices.IndexFunc(built, func(b cognition.BuiltIn) bool { return b.ID == id }); i >= 0 {
				return built[i].Graph, true
			}
			return cognition.Graph{}, false
		},
		Entity: func(id string) (records.EntityType, bool) {
			var d entities.Draft
			return d.Entity, read(entities.DraftProjector, entities.TargetType+"."+id, &d)
		},
	}
}

// env reads the parts through the decision, so a part this process
// changed just before (a prefab made in the same gesture) is read as
// changed, and an undo's parts as its earlier ops left them.
func (c *decision) env() apps.Env {
	if c.Read == nil {
		return Env(c.Context(), c.deps.Projections, c.Principal.TenantID, c.reg)
	}
	return envFrom(func(projector, key string) (json.RawMessage, bool) {
		typ, id, _ := strings.Cut(key, ".")
		v, err := c.Read(projector, live.Target{Area: live.AreaDefinitions, Type: typ, ID: id})
		return v, err == nil && v != nil
	}, c.reg)
}

// slug is an app's id made from its name: "My tasks" is myTasks.
func slug(name string) string {
	var b strings.Builder
	up := false
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if b.Len() == 0 {
				if unicode.IsDigit(r) {
					b.WriteString("a")
				}
				b.WriteRune(unicode.ToLower(r))
			} else if up {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
			up = false
		default:
			up = true
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func newID(base string, taken func(string) bool) string {
	id := base
	for n := 2; taken(id); n++ {
		id = base + "_" + strconv.Itoa(n)
	}
	return id
}

// The install projection and target type (apps/installs, which imports
// this package), read before an app is deleted.
const (
	installProjector = "apps.install"
	installType      = "appInstall"
)

type spec struct {
	id, title, description, args, example string
	events                                []string
	create                                bool   // may target an app that does not exist yet
	action                                string // app.write when empty
	decide                                func(c *decision) ([]live.NewEvent, []live.Problem)
}

// Contribute registers the apps' projector, commands and queries; deps
// returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: VersionProjector, Source: "apps"}, Title: "App versions",
		Description: "Every published app version, keyed appVersion.<appId>-v<n>: {app, version, rev, manifest, prefabs, entities, graphs, publishedBy, publishedAt}",
		Area:        live.AreaDefinitions, Types: []string{VersionTargetType}, Fold: foldVersion,
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: DraftProjector, Source: "apps"}, Title: "App drafts",
		Description: "Each app's draft, as its app.* commands left it: {manifest, rev, createdBy, updatedBy, updatedAt}",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: fold,
	})
	for _, s := range commandSpecs() {
		cmd := live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "apps"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events,
			Area: live.AreaDefinitions, TargetType: TargetType, Projection: DraftProjector, Action: cmp.Or(s.action, "app.write"),
			Resource: func(p features.Principal, t live.Target) string {
				if s.create {
					return apps.Resource(p, "")
				}
				return apps.Resource(p, t.ID)
			},
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				c := &decision{Decision: d, deps: deps(), reg: reg}
				if d.State != nil {
					if err := json.Unmarshal(d.State, &c.draft); err != nil {
						return nil, problem("", "the draft cannot be read: "+err.Error(), "")
					}
					c.exists = true
				}
				if !c.exists && !s.create {
					return nil, problem("target", fmt.Sprintf("there is no app %q", d.Target.ID), "make it first with app.create {name}")
				}
				if c.draft.Deleted && s.id != "app.restore" {
					return nil, problem("target", fmt.Sprintf("the app %q is deleted", d.Target.ID), "bring it back with app.restore first")
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
			Report: func(args features.ServiceArgs, t live.Target, seq uint64) any {
				if s.id == "app.publish" {
					// An install right after publishing finds the new version
					if r, ok := report(args, deps(), t.ID, seq).(Report); ok {
						key := VersionTargetType + "." + apps.VersionID(t.ID, r.Published)
						_, _ = deps().Projections.WaitFor(args.Context(), args.Principal.TenantID, VersionProjector, key, seq, 2*time.Second)
						return r
					}
				}
				return report(args, deps(), t.ID, seq)
			},
		}
		if s.create {
			cmd.NewTargetFor = func(args features.ServiceArgs, raw json.RawMessage) string {
				var a struct{ Name string }
				_ = json.Unmarshal(raw, &a)
				return newID(slug(a.Name), func(id string) bool {
					_, ok := get(args.Context(), deps().Projections, args.Principal.TenantID, DraftProjector, TargetType+"."+id)
					return ok
				})
			}
		}
		live.Commands.Register(reg, cmd)
	}
	contributeQueries(reg, deps)
	contributeVersions(reg, deps)
}

// check applies the events to a copy of the draft and checks the whole
// manifest, so no command leaves a problem in the draft.
func (c *decision) check(events []live.NewEvent) []live.Problem {
	raw, _ := json.Marshal(c.draft)
	var d Draft
	_ = json.Unmarshal(raw, &d)
	for _, e := range events {
		if e.Target != nil {
			continue // another target's, such as a published version
		}
		data, err := json.Marshal(e.Data)
		if err == nil {
			err = Apply(&d, live.Event{Type: e.Type, Target: c.Target, Data: data})
		}
		if err != nil {
			return problem("", err.Error(), "")
		}
	}
	problems := apps.CheckManifest(d.Manifest, c.env())
	for i := range problems {
		problems[i].Path = strings.TrimSuffix("manifest."+problems[i].Path, ".")
	}
	return problems
}

// Report is what an app.* command's tool returns: the draft's rev, its
// problems (none: a command leaving any is refused) and the keys of its
// lists, so a model sees what is there.
type Report struct {
	Rev       int                 `json:"rev"`
	Published int                 `json:"published"` // the last published version
	Problems  []live.Problem      `json:"problems"`
	Keys      map[string][]string `json:"keys"`
}

func report(args features.ServiceArgs, deps Deps, id string, seq uint64) any {
	v, err := deps.Projections.WaitFor(args.Context(), args.Principal.TenantID, DraftProjector, TargetType+"."+id, seq, 2*time.Second)
	if err != nil || v.Value == nil {
		return nil
	}
	var d Draft
	if json.Unmarshal(v.Value, &d) != nil {
		return nil
	}
	keys := map[string][]string{}
	for _, l := range Lists {
		keys[l.Field] = Keys(d.Manifest, l.Item)
	}
	return Report{Rev: d.Rev, Published: d.Published, Problems: []live.Problem{}, Keys: keys}
}

const requiresSchema = `"requires":{"type":"object","description":"{primitives: [component ids or families such as mui], apps: [app ids]}: what a tenant needs before the app installs","properties":{"primitives":{"type":"array","items":{"type":"string"}},"apps":{"type":"array","items":{"type":"string"}}}}`

func commandSpecs() []spec {
	specs := []spec{
		{
			id: "app.create", title: "Create an app", create: true, events: []string{EventCreated},
			description: "Makes a new application: a manifest of pages (routes to prefabs), navigation, editor kinds, entity types, commands, queries, roles, settings and triggers. Its id is the target, or made from the name when left out (My tasks is myTasks). Its parts (prefabs, graphs, entity types) are made with their own commands with owner app:<appId>, then named in the manifest with app.addPage, app.addEntity, app.addCommand and the rest.",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"icon":{"type":"string","description":"A Material icon name, such as checklist"},"description":{"type":"string","description":"What the app is for, for a model"}},"required":["name"]}`,
			example:     `{"name":"Tasks","icon":"checklist","description":"Tasks assigned to people, with due dates"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if c.exists {
					return nil, problem("target", fmt.Sprintf("there is already an app %q", c.Target.ID), "choose another id, or change it with the other app.* commands")
				}
				a, problems := args[struct{ Name, Icon, Description string }](c)
				if problems != nil {
					return nil, problems
				}
				// The copy into the tenant database runs before the event lands
				if err := Follow(c.ServiceArgs, c.deps); err != nil {
					return nil, problem("", "the app cannot be kept: "+err.Error(), "")
				}
				m := apps.Manifest{ID: c.Target.ID, Name: a.Name, Icon: a.Icon, Description: a.Description}
				apps.Normalize(&m)
				return ev(EventCreated, map[string]any{"manifest": m}), nil
			},
		},
		{
			id: "app.delete", title: "Delete an app", events: []string{EventDeleted},
			description: "Deletes an app: it is hidden with its own parts, not erased, and app.restore brings it back. Refused while the app is installed: uninstall it first.",
			args:        `{"type":"object","properties":{}}`,
			example:     `{}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				raw, err := c.Read(installProjector, live.Target{Area: live.AreaDefinitions, Type: installType, ID: c.Target.ID})
				var in apps.Install
				if err == nil && raw != nil && json.Unmarshal(raw, &in) == nil && in.Installed() {
					return nil, problem("target", fmt.Sprintf("the app %q is installed", c.Target.ID), "uninstall it first with app.uninstall")
				}
				return ev(EventDeleted, map[string]any{}), nil
			},
		},
		{
			id: "app.restore", title: "Restore an app", events: []string{EventRestored},
			description: "Brings back a deleted app with its own parts, as it was.",
			args:        `{"type":"object","properties":{}}`,
			example:     `{}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if !c.draft.Deleted {
					return nil, problem("target", fmt.Sprintf("the app %q is not deleted", c.Target.ID), "")
				}
				return ev(EventRestored, map[string]any{}), nil
			},
		},
		{
			id: "app.setInfo", title: "Describe an app", events: []string{EventInfoSet},
			description: "Sets an app's name, icon, description, home (the route it opens on; empty for its first page's), frame (prefab.<id> wrapping every page; empty for the default navigation rail and header) and requires (replaced whole).",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"icon":{"type":"string"},"description":{"type":"string"},"home":{"type":"string"},"frame":{"type":"string"},` + requiresSchema + `}}`,
			example:     `{"home":"/mine","requires":{"primitives":["mui"]}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				in, problems := args[Info](c)
				if problems != nil {
					return nil, problems
				}
				m := c.manifest()
				same := func(v *string, cur string) bool { return v == nil || *v == cur }
				if same(in.Name, m.Name) && same(in.Icon, m.Icon) && same(in.Description, m.Description) && same(in.Home, m.Home) &&
					same(in.Frame, m.Frame) && (in.Requires == nil || equal(*in.Requires, m.Requires)) {
					return nil, nil
				}
				return ev(EventInfoSet, in), nil
			},
		},
		{
			id: "app.setNav", title: "Set an app's navigation", events: []string{EventNavSet},
			description: "Sets the navigation its frame shows, replacing it whole: items {title, route, icon?} in order, each route a page's without params.",
			args:        `{"type":"object","properties":{"nav":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"route":{"type":"string"},"icon":{"type":"string"}},"required":["title","route"]}}},"required":["nav"]}`,
			example:     `{"nav":[{"title":"My tasks","route":"/mine","icon":"person"},{"title":"All tasks","route":"/all","icon":"list"}]}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Nav []apps.NavItem }](c)
				if problems != nil {
					return nil, problems
				}
				if a.Nav == nil {
					a.Nav = []apps.NavItem{}
				}
				if equal(a.Nav, c.manifest().Nav) {
					return nil, nil
				}
				return ev(EventNavSet, map[string]any{"nav": a.Nav}), nil
			},
		},
		{
			id: "app.setSettingsSchema", title: "Set an app's settings", events: []string{EventSettingsSet},
			description: "Sets the app's settings, replacing them: a JSON Schema object (with title and description per property) and the defaults. Once the app is installed they are the configuration section app.<appId>, set per tenant and per user.",
			args:        `{"type":"object","properties":{"schema":{"type":"object","description":"A JSON Schema with type object"},"defaults":{"type":"object"}},"required":["schema"]}`,
			example:     `{"schema":{"type":"object","properties":{"defaultDueDays":{"type":"integer","title":"Default due in N days","minimum":0}}},"defaults":{"defaultDueDays":7}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[apps.Settings](c)
				if problems != nil {
					return nil, problems
				}
				if equal(a, c.manifest().Settings) {
					return nil, nil
				}
				return ev(EventSettingsSet, map[string]any{"settings": a}), nil
			},
		},
	}
	specs = append(specs, spec{
		id: "app.setManifest", title: "Replace an app's manifest", events: []string{EventManifestSet},
		description: "Replaces the app's whole manifest (pages, nav, home, frame, editor kinds, entity types, commands, queries, roles, settings, triggers, requires), checked whole against its parts. Importing an app (app.import) uses it; to change one item, use its own command.",
		args:        `{"type":"object","properties":{"manifest":{"type":"object","description":"A manifest as app.describe gives it; its id and version are the app's own"}},"required":["manifest"]}`,
		example:     `{"manifest":{"name":"Tasks","pages":[{"route":"/mine","prefab":"myTasks"}],"entities":["task"]}}`,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := args[struct{ Manifest *apps.Manifest }](c)
			if problems != nil {
				return nil, problems
			}
			if a.Manifest == nil {
				return nil, problem("args.manifest", "the manifest is missing", "give the manifest as app.describe returns it")
			}
			m := *a.Manifest
			m.ID, m.Version = c.Target.ID, c.draft.Published
			apps.Normalize(&m)
			if equal(m, c.draft.Manifest) {
				return nil, nil
			}
			return ev(EventManifestSet, map[string]any{"manifest": m}), nil
		},
	}, spec{
		id: "app.setSeed", title: "Set an app's sample records", events: []string{EventSeedSet},
		description: "Sets the sample records the next published version carries, replacing them: entity type → records {id, values}. Installing the version creates them in each of the app's own entity types that has no records yet.",
		args:        `{"type":"object","properties":{"seed":{"type":"object","description":"entity type → [{id, values: {field id → value}}]"}},"required":["seed"]}`,
		example:     `{"seed":{"task":[{"id":"t1","values":{"title":"Write the plan"}}]}}`,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := args[struct{ Seed apps.Seed }](c)
			if problems != nil {
				return nil, problems
			}
			for entity, recs := range a.Seed {
				if !slices.Contains(c.manifest().Entities, entity) {
					return nil, problem("args.seed."+entity, fmt.Sprintf("the app does not use the entity type %s", entity), "add it with app.addEntity first, or leave its records out")
				}
				for i, r := range recs {
					if !live.ValidToken(r.ID) {
						return nil, problem(fmt.Sprintf("args.seed.%s.%d.id", entity, i), fmt.Sprintf("%q is not a record id", r.ID), "give each record its id")
					}
				}
			}
			if len(a.Seed) == 0 {
				a.Seed = nil
			}
			if equal(a.Seed, c.draft.Seed) {
				return nil, nil
			}
			return ev(EventSeedSet, map[string]any{"seed": a.Seed}), nil
		},
	})
	for _, l := range Lists {
		specs = append(specs, listSpecs(l)...)
	}
	return append(specs, publishSpec())
}

// listSpecs are a list's add, update and remove commands.
func listSpecs(l List) []spec {
	keyArg := l.Key
	if keyArg == "" {
		keyArg = l.Arg
	}
	t := title(l.Item)
	required := append([]string{keyArg}, l.Required...)
	reqJSON, _ := json.Marshal(required)
	add := spec{
		id: "app.add" + t, title: "Add a " + l.Title, events: []string{l.Added()},
		description: fmt.Sprintf("Adds a %s to the app: %s. Each %s is named by its %s; index puts it there (the end when left out).", l.Title, l.Describe, l.Title, keyArg),
		args:        `{"type":"object","properties":{` + l.Props + `,"index":{"type":"integer"}},"required":` + string(reqJSON) + `}`,
		example:     l.Example,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := args[map[string]any](c)
			if problems != nil {
				return nil, problems
			}
			index := -1
			if n, ok := a["index"].(float64); ok {
				index = int(n)
			}
			delete(a, "index")
			key, _ := a[keyArg].(string)
			var item any = a
			if l.Key == "" {
				item = key
			}
			if slices.Contains(Keys(*c.manifest(), l.Item), key) {
				return nil, problem("args."+keyArg, fmt.Sprintf("there is already a %s %s", l.Title, key), fmt.Sprintf("change it with app.update%s, or choose another %s", t, keyArg))
			}
			return ev(l.Added(), Added{Item: item, Index: index}), nil
		},
	}
	remove := spec{
		id: "app.remove" + t, title: "Remove a " + l.Title, events: []string{l.Removed()},
		description: fmt.Sprintf("Removes a %s from the app, by its %s. Its parts (prefabs, graphs, entity types) stay; what names it must be changed first.", l.Title, keyArg),
		args:        `{"type":"object","properties":{"` + keyArg + `":{"type":"string"}},"required":["` + keyArg + `"]}`,
		example:     `{"` + keyArg + `":` + mustKey(l.Example, keyArg) + `}`,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := args[map[string]any](c)
			if problems != nil {
				return nil, problems
			}
			key, _ := a[keyArg].(string)
			if problems := c.item(l, key, "args."+keyArg); problems != nil {
				return nil, problems
			}
			return ev(l.Removed(), map[string]any{"key": key}), nil
		},
	}
	if l.Key == "" {
		return []spec{add, remove}
	}
	update := spec{
		id: "app.update" + t, title: "Change a " + l.Title, events: []string{l.Updated()},
		description: fmt.Sprintf("Changes the %s named by %s: each field given is set, one given as null is cleared, objects merge per key and lists are replaced, so authors changing different fields of one %s do not overwrite each other. rename gives it a new %s.", l.Title, keyArg, l.Title, keyArg),
		args:        `{"type":"object","properties":{` + l.Props + `,"rename":{"type":"string","description":"A new ` + keyArg + `"}},"required":["` + keyArg + `"]}`,
		example:     l.Update,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := args[map[string]any](c)
			if problems != nil {
				return nil, problems
			}
			key, _ := a[keyArg].(string)
			if problems := c.item(l, key, "args."+keyArg); problems != nil {
				return nil, problems
			}
			patch := map[string]any{}
			for k, v := range a {
				if k != keyArg && k != "rename" {
					patch[k] = v
				}
			}
			if to, ok := a["rename"].(string); ok && to != key {
				if slices.Contains(Keys(*c.manifest(), l.Item), to) {
					return nil, problem("args.rename", fmt.Sprintf("there is already a %s %s", l.Title, to), "")
				}
				patch[l.Key] = to
			}
			// Only what changes goes in the event
			cur := l.items(asDoc(*c.manifest()))[l.find(asDoc(*c.manifest()), key)]
			obj, _ := cur.(map[string]any)
			for k, v := range patch {
				if old, ok := obj[k]; ok == (v != nil) && equal(Merge(old, v), old) {
					delete(patch, k)
				}
			}
			if len(patch) == 0 {
				return nil, nil
			}
			return ev(l.Updated(), Updated{Key: key, Patch: patch}), nil
		},
	}
	return []spec{add, update, remove}
}

// item checks that the draft has the item at key.
func (c *decision) item(l List, key, arg string) []live.Problem {
	keys := Keys(*c.manifest(), l.Item)
	if slices.Contains(keys, key) {
		return nil
	}
	fix := fmt.Sprintf("the app has no %s yet", l.Field)
	if len(keys) > 0 {
		fix = fmt.Sprintf("the app's %s are %s", l.Field, strings.Join(keys, ", "))
	}
	return problem(arg, fmt.Sprintf("there is no %s %q", l.Title, key), fix)
}

// mustKey is the key's value in an example, as JSON.
func mustKey(example, key string) string {
	var v map[string]any
	_ = json.Unmarshal([]byte(example), &v)
	raw, _ := json.Marshal(v[key])
	return string(raw)
}

func equal(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
