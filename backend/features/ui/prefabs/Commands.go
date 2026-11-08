package prefabs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"gorm.io/gorm"
)

var log = ui.Log.Tags("prefabs")

// Projections are what prefabs read: the projections, waiting for a
// commit to be projected, and following the def stream
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
}

// decision is one command's view of its prefab.
type decision struct {
	live.Decision
	deps   Deps
	reg    *extensions.Registry
	draft  Draft
	exists bool
}

func (c *decision) prefab() *ui.Prefab { return &c.draft.Prefab }

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

func (c *decision) get(projector, key string) (json.RawMessage, bool) {
	// Through the decision, so a part this process changed just before
	// is read as changed
	if typ, id, ok := strings.Cut(key, "."); ok && c.Read != nil {
		v, err := c.Read(projector, live.Target{Area: live.AreaDefinitions, Type: typ, ID: id})
		return v, err == nil && v != nil
	}
	return get(c.Context(), c.deps.Projections, c.Principal.TenantID, projector, key)
}

// published is a prefab's last published version.
func (c *decision) published(id string) (ui.Prefab, bool) {
	var p ui.Prefab
	raw, ok := c.get(PublishedProjector, TargetType+"."+id)
	return p, ok && json.Unmarshal(raw, &p) == nil
}

// drafted is a prefab's draft.
func (c *decision) drafted(id string) (ui.Prefab, bool) {
	var d Draft
	raw, ok := c.get(DraftProjector, TargetType+"."+id)
	return d.Prefab, ok && json.Unmarshal(raw, &d) == nil
}

// env is what the prefab is checked against: the catalog and the tenant's
// other prefabs, entity types, queries and graphs; owner is the prefab's
// as the command leaves it, whose app's own queries it may read.
func (c *decision) env(owner string) ui.PrefabEnv {
	return ui.PrefabEnv{
		Catalog: ui.Catalog(c.reg),
		Prefab: func(id string) (ui.Prefab, bool) {
			if !live.ValidToken(id) {
				return ui.Prefab{}, false
			}
			if p, ok := c.published(id); ok {
				return p, true
			}
			return c.drafted(id)
		},
		Uses: func(id string) []string {
			var out []string
			if p, ok := c.drafted(id); ok {
				out = ui.PrefabsUsed(p.Template)
			}
			if p, ok := c.published(id); ok {
				out = append(out, ui.PrefabsUsed(p.Template)...)
			}
			return out
		},
		Version: func(id string, version int) (ui.Prefab, bool) {
			p, _, err := Version(c.ServiceArgs, c.deps, id, version)
			return p, err == nil
		},
		Entity: func(id string) bool {
			_, ok := c.get(entities.DraftProjector, entities.TargetType+"."+id)
			return live.ValidToken(id) && ok
		},
		Query: func(name string) bool {
			if slices.ContainsFunc(live.Queries.All(c.reg), func(q live.Query) bool { return q.ID == name }) {
				return true
			}
			if _, ok := c.graphKind(livenodes.QueryGraphID(name)); ok {
				return true
			}
			// An app's own query, whose graph has the app's id in its own
			app, ok := strings.CutPrefix(owner, "app:")
			if ok {
				_, ok = c.graphKind(livenodes.QueryGraphID(app + "." + name))
			}
			return ok
		},
		Graph: c.graphKind,
	}
}

// graphKind is a graph's kind, from its draft, its published version or
// the built-in graph of that id (an app's graphs, such as Hello's).
func (c *decision) graphKind(id string) (string, bool) {
	if !live.ValidToken(id) {
		return "", false
	}
	var d drafts.Draft
	if raw, ok := c.get(drafts.DraftProjector, drafts.TargetType+"."+id); ok && json.Unmarshal(raw, &d) == nil {
		return d.Graph.Kind, true
	}
	var g struct{ Kind string }
	if raw, ok := c.get(drafts.PublishedProjector, drafts.TargetType+"."+id); ok && json.Unmarshal(raw, &g) == nil {
		return g.Kind, true
	}
	built := cognition.Graphs.All(c.reg)
	if i := slices.IndexFunc(built, func(b cognition.BuiltIn) bool { return b.ID == id }); i >= 0 {
		return built[i].Graph.Kind, true
	}
	return "", false
}

// slug is a prefab's id made from its name: "Task row" is taskRow.
func slug(name string) string {
	var b strings.Builder
	up := false
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if b.Len() == 0 {
				if unicode.IsDigit(r) {
					b.WriteString("p")
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
		return "prefab"
	}
	return b.String()
}

// newID is an id from base that taken does not hold.
func newID(base string, taken func(string) bool) string {
	id := base
	for n := 2; taken(id); n++ {
		id = base + "_" + strconv.Itoa(n)
	}
	return id
}

// keys is every key in a template.
func keys(root *ui.Node) map[string]bool {
	out := map[string]bool{}
	walk(root, func(n *ui.Node) { out[n.Key] = true })
	return out
}

func walk(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walk(c, fn)
	}
	for _, list := range n.Slots {
		for _, c := range list {
			walk(c, fn)
		}
	}
}

// keyed gives every node of a new subtree without a key one from its
// type (checkbox, checkbox_2), new in the whole template.
func keyed(n *ui.Node, taken map[string]bool) {
	walk(n, func(n *ui.Node) {
		if n.Key == "" {
			typ, _, _ := strings.Cut(n.Type, "@")
			base := typ[strings.LastIndex(typ, ".")+1:]
			if !live.ValidToken(base) {
				base = "node"
			}
			n.Key = newID(base, func(k string) bool { return taken[k] })
		}
		taken[n.Key] = true
	})
}

// node is the node at a path of the draft's template.
func (c *decision) node(path, arg string) (place, []live.Problem) {
	at, ok := find(c.prefab().Template, path)
	if !ok {
		fix := "the tree is empty: insert its root first, with no parentPath"
		if paths := Paths(c.prefab().Template); len(paths) > 0 {
			if len(paths) > 40 {
				paths = append(paths[:40], "…")
			}
			fix = "paths are node keys from the root joined with '/'; this prefab's nodes are " + strings.Join(paths, ", ")
		}
		return at, problem(arg, fmt.Sprintf("there is no node at %q", path), fix)
	}
	return at, nil
}

type spec struct {
	id, title, description, args, example string
	events                                []string
	action                                string
	create                                bool // may target a prefab that does not exist yet
	decide                                func(c *decision) ([]live.NewEvent, []live.Problem)
}

// Contribute registers the prefabs' projectors, commands and queries;
// deps returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: DraftProjector, Source: "ui"}, Title: "Prefab drafts",
		Description: "Each prefab's draft, as its prefab.* commands left it: {prefab, rev, published, publishedRev, updatedBy, updatedAt}",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: fold,
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: PublishedProjector, Source: "ui"}, Title: "Published prefabs",
		Description: "Each prefab's last published version, the one its instances render",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: foldPublished,
	})
	// The Prefab Editor's Canvas draws a draft on this page (forge 04c)
	ui.Pages.Register(reg, ui.DesignPage())

	for _, s := range commandSpecs() {
		action := s.action
		if action == "" {
			action = "prefabs.write"
		}
		cmd := live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "ui"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events,
			Area: live.AreaDefinitions, TargetType: TargetType, Projection: DraftProjector, Action: action,
			Resource: func(p features.Principal, t live.Target) string { return ui.PrefabResource(p, t.ID) },
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				c := &decision{Decision: d, deps: deps(), reg: reg}
				if d.State != nil {
					if err := json.Unmarshal(d.State, &c.draft); err != nil {
						return nil, problem("", "the draft cannot be read: "+err.Error(), "")
					}
					c.exists = true
				}
				if !c.exists && !s.create {
					return nil, problem("target", fmt.Sprintf("there is no prefab %q", d.Target.ID), "make it first with prefab.create {name}")
				}
				if c.draft.Deleted && s.id != "prefab.restore" {
					return nil, problem("target", fmt.Sprintf("the prefab %q is deleted", d.Target.ID), "bring it back with prefab.restore first")
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
		}
		if s.id == "prefab.publish" {
			// A preview right after publishing renders the new version
			cmd.Report = func(args features.ServiceArgs, t live.Target, seq uint64) any {
				_, _ = deps().Projections.WaitFor(args.Context(), args.Principal.TenantID, PublishedProjector, TargetType+"."+t.ID, seq, 2*time.Second)
				return report(args, deps(), t.ID, seq)
			}
		}
		if s.create {
			cmd.NewTargetFor = func(args features.ServiceArgs, raw json.RawMessage) string {
				var a struct {
					Name   string
					Prefab struct{ Name string } // prefab.import's
				}
				_ = json.Unmarshal(raw, &a)
				if a.Name == "" {
					a.Name = a.Prefab.Name
				}
				return newID(slug(a.Name), func(id string) bool {
					_, ok := get(args.Context(), deps().Projections, args.Principal.TenantID, DraftProjector, TargetType+"."+id)
					return ok
				})
			}
		}
		live.Commands.Register(reg, cmd)
	}
	contributeQueries(reg, deps)
}

// check applies the events to a copy of the draft and checks the whole
// definition, so no command leaves an invalid draft.
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
	problems := ui.CheckPrefab(d.Prefab, c.env(d.Prefab.Owner))
	for i := range problems {
		problems[i].Path = strings.TrimSuffix("prefab."+problems[i].Path, ".")
	}
	return problems
}

// Report is what a prefab.* command's tool returns: the draft's rev and
// published version, its problems (none: a command leaving any is
// refused), and the template's node paths, so a model sees the key a new
// node got.
type Report struct {
	Rev       int            `json:"rev"`
	Published int            `json:"published"`
	Problems  []live.Problem `json:"problems"`
	Nodes     []string       `json:"nodes"`
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
	return Report{Rev: d.Rev, Published: d.Published, Problems: []live.Problem{}, Nodes: Paths(d.Prefab.Template)}
}

// Paths are a template's node paths, depth first.
func Paths(root *ui.Node) []string {
	out := []string{}
	var visit func(n *ui.Node, path string)
	visit = func(n *ui.Node, path string) {
		path = strings.TrimPrefix(path+"/"+n.Key, "/")
		out = append(out, path)
		for _, c := range n.Children {
			visit(c, path)
		}
		for _, s := range slices.Sorted(mapKeys(n.Slots)) {
			for _, c := range n.Slots[s] {
				visit(c, path)
			}
		}
	}
	if root != nil {
		visit(root, "")
	}
	return out
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

const nodeSchema = `{"type":"object","description":"A ui.Node: {type, key?, variant?, props?, bind?, on?, children?, slots?}. type is a component (ui.listComponents), slot (props.name: a declared slot) or prefab.<id>; a key is made from the type when left out","properties":{"type":{"type":"string"},"key":{"type":"string"},"variant":{"type":"string","description":"A prefab instance's variant: one its prefab declares"},"props":{"type":"object"},"bind":{"type":"object","description":"prop → props.<prop>, state.<value>, slots.<slot>, query:<name>[.path] or local.<path>"},"on":{"type":"object","description":"event → {handler: a name in the prefab's graphs, payload?}"},"children":{"type":"array","items":{"type":"object"}},"slots":{"type":"object"}},"required":["type"]}`

const ifaceSchema = `"props":{"type":"object","description":"name → {type, entity?, options?, description, required?, default?}; type is one of string, number, integer, bool, date, dateTime, enum, json, record, records"},` +
	`"events":{"type":"object","description":"name → {description, payload: {field: type}}"},` +
	`"slots":{"type":"object","description":"name → {description}"},` +
	`"state":{"type":"object","description":"name → {type, default?, description}: each instance's own view state"},` +
	`"queries":{"type":"object","description":"name → {query, params?, bind?: {param: props.<prop>|state.<value>}}: a registered query or data.query graph the instance holds"}`

func commandSpecs() []spec {
	return []spec{
		{
			id: "prefab.create", title: "Create a prefab", create: true, events: []string{EventCreated},
			description: "Makes a new prefab (a reusable component: a tree of components with props, events, slots, state, queries and the graphs behind it). Its id is the target, or made from the name when left out (Task row is taskRow). owner app:<appId> puts it in an app; without one it is in the tenant's library. Then declare its interface with prefab.setInterface and build its tree with prefab.insertNode.",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string","description":"What it shows and when to use it, for a model"},"owner":{"type":"string","description":"app:<appId>, or none for the tenant's library"}},"required":["name"]}`,
			example:     `{"name":"Task row","description":"One task with a checkbox and its title","owner":"app:hello"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if c.exists {
					return nil, problem("target", fmt.Sprintf("there is already a prefab %q", c.Target.ID), "choose another id, or change it with the other prefab.* commands")
				}
				a, problems := args[struct{ Name, Description, Owner string }](c)
				if problems != nil {
					return nil, problems
				}
				p := ui.Prefab{ID: c.Target.ID, Name: a.Name, Description: a.Description, Owner: a.Owner}
				normalize(&p)
				return ev(EventCreated, map[string]any{"prefab": p}), nil
			},
		},
		{
			id: "prefab.import", title: "Import a prefab", create: true, events: []string{EventImported},
			description: "Replaces the target prefab's whole draft with a definition given as JSON (its name, description, owner, interface, template, graphs and preview, as prefab.describe gives it), or makes it. Its published versions stay until it is published again. Seeding a tenant with ready-made prefabs uses it.",
			args:        `{"type":"object","properties":{"prefab":{"type":"object","description":"A prefab as prefab.describe returns it: {name, description?, owner?, props, events, slots, state, queries, template, graphs, preview}"}},"required":["prefab"]}`,
			example:     `{"prefab":{"name":"Empty state","props":{"title":{"type":"string","default":"Nothing here"}},"template":{"type":"mui.empty","key":"root","bind":{"title":"props.title"}}}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Prefab *ui.Prefab }](c)
				if problems != nil {
					return nil, problems
				}
				if a.Prefab == nil {
					return nil, problem("args.prefab", "the definition is missing", "give the prefab as prefab.describe returns it")
				}
				p := *a.Prefab
				p.ID, p.Version = c.Target.ID, 0
				normalize(&p)
				if c.exists {
					cur := *c.prefab()
					cur.Version = 0
					if equal(cur, p) {
						return nil, nil
					}
				}
				return ev(EventImported, map[string]any{"prefab": p}), nil
			},
		},
		{
			id: "prefab.setInfo", title: "Describe a prefab", events: []string{EventInfoSet},
			description: "Sets a prefab's name, description and owner (app:<appId>, or \"\" for the tenant's library).",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"owner":{"type":"string"}}}`,
			example:     `{"description":"One task with a checkbox, its title and its due date"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				in, problems := args[Info](c)
				if problems != nil || in == (Info{}) {
					return nil, problems
				}
				return ev(EventInfoSet, in), nil
			},
		},
		{
			id: "prefab.setInterface", title: "Set a prefab's interface", events: []string{EventInterfaceSet},
			description: "Declares a prefab's props, events and slots (what its users see), its state and its queries. Each section given replaces that section; one left out stays. Removing a prop, event or slot, or changing a prop's type, breaks published prefabs that use it, which prefab.publish reports.",
			args:        `{"type":"object","properties":{` + ifaceSchema + `}}`,
			example:     `{"props":{"task":{"type":"record","entity":"task","description":"The task to show","required":true}},"events":{"toggled":{"description":"The checkbox was clicked","payload":{"id":"string","done":"bool"}}},"slots":{"actions":{"description":"Buttons at the row's end"}}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				in, problems := args[Interface](c)
				if problems != nil || in == (Interface{}) {
					return nil, problems
				}
				return ev(EventInterfaceSet, in), nil
			},
		},
		{
			id: "prefab.insertNode", title: "Insert a node", events: []string{EventNodeInserted},
			description: "Inserts a component node (with its subtree) into a prefab's tree, under the node at parentPath (into its slot when slot is given), at index (the end when left out). With no parentPath it becomes the root of an empty tree. Node paths are keys from the root joined with '/', such as root/row/title; a node without a key gets one from its type (checkbox, checkbox_2), and the result lists every node's path.",
			args:        `{"type":"object","properties":{"parentPath":{"type":"string"},"slot":{"type":"string","description":"A slot of the parent, such as a card's actions"},"index":{"type":"integer"},"node":` + nodeSchema + `},"required":["node"]}`,
			example:     `{"parentPath":"root","node":{"type":"mui.checkbox","key":"done","bind":{"checked":"props.task.done"},"on":{"change":{"handler":"onToggle"}}}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct {
					ParentPath, Slot string
					Index            *int
					Node             *ui.Node
				}](c)
				if problems != nil {
					return nil, problems
				}
				if a.Node == nil || a.Node.Type == "" {
					return nil, problem("args.node.type", "a node needs a type", "use a component ui.listComponents lists, slot, or prefab.<id>")
				}
				in := Insert{ParentPath: a.ParentPath, Slot: a.Slot, Index: -1, Node: a.Node}
				if a.Index != nil {
					in.Index = *a.Index
				}
				switch {
				case a.ParentPath == "" && c.prefab().Template != nil:
					return nil, problem("args.parentPath", "the tree already has a root", "insert under a node, such as "+c.prefab().Template.Key)
				case a.ParentPath != "":
					if _, problems := c.node(a.ParentPath, "args.parentPath"); problems != nil {
						return nil, problems
					}
				}
				keyed(a.Node, keys(c.prefab().Template))
				return ev(EventNodeInserted, in), nil
			},
		},
		{
			id: "prefab.updateNode", title: "Change a node", events: []string{EventNodeUpdated},
			description: "Changes the node at path: each prop, binding and event handler given is set, one given as null is cleared, and one left out stays, so designers changing different props of one node do not overwrite each other. key renames the node, which changes its path and its subtree's. variant picks a prefab instance's variant (null clears it); its own props override the variant's.",
			args:        `{"type":"object","properties":{"path":{"type":"string"},"props":{"type":"object"},"bind":{"type":"object","description":"prop → path, or null to clear"},"on":{"type":"object","description":"event → {handler, payload?}, or null to clear"},"key":{"type":"string"},"variant":{"type":"string","description":"One of the instanced prefab's variants, or null for none"}},"required":["path"]}`,
			example:     `{"path":"root/title","props":{"variant":"body2"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				u, problems := args[Update](c)
				if problems != nil {
					return nil, problems
				}
				// null clears the variant; the event says so with ""
				var raw struct{ Variant json.RawMessage }
				_ = json.Unmarshal(c.Args, &raw)
				if string(raw.Variant) == "null" {
					u.Variant = new(string)
				}
				at, problems := c.node(u.Path, "args.path")
				if problems != nil {
					return nil, problems
				}
				n := at.node
				for k, v := range u.Props {
					if old, ok := n.Props[k]; ok == (v != nil) && equal(old, v) {
						delete(u.Props, k)
					}
				}
				for k, v := range u.Bind {
					if old, ok := n.Bind[k]; ok == (v != nil) && (v == nil || old == *v) {
						delete(u.Bind, k)
					}
				}
				for k, v := range u.On {
					if old, ok := n.On[k]; ok == (v != nil) && (v == nil || equal(old, *v)) {
						delete(u.On, k)
					}
				}
				if u.Key == n.Key {
					u.Key = ""
				}
				if u.Variant != nil && *u.Variant == n.Variant {
					u.Variant = nil
				}
				if len(u.Props)+len(u.Bind)+len(u.On) == 0 && u.Key == "" && u.Variant == nil {
					return nil, nil
				}
				return ev(EventNodeUpdated, u), nil
			},
		},
		{
			id: "prefab.moveNode", title: "Move a node", events: []string{EventNodeMoved},
			description: "Moves the node at path, with its subtree, under the node at parentPath (into its slot when slot is given), at index of that list once it has left its place (the end when left out).",
			args:        `{"type":"object","properties":{"path":{"type":"string"},"parentPath":{"type":"string"},"slot":{"type":"string"},"index":{"type":"integer"}},"required":["path","parentPath"]}`,
			example:     `{"path":"root/due","parentPath":"root","index":1}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct {
					Path, ParentPath, Slot string
					Index                  *int
				}](c)
				if problems != nil {
					return nil, problems
				}
				at, problems := c.node(a.Path, "args.path")
				if problems != nil {
					return nil, problems
				}
				if at.parent == nil {
					return nil, problem("args.path", "the root cannot be moved", "wrap it with prefab.wrapNodes, or move its children")
				}
				if a.ParentPath == a.Path || strings.HasPrefix(a.ParentPath, a.Path+"/") {
					return nil, problem("args.parentPath", "a node cannot move into itself", "")
				}
				if _, problems := c.node(a.ParentPath, "args.parentPath"); problems != nil {
					return nil, problems
				}
				m := Move{Path: a.Path, ParentPath: a.ParentPath, Slot: a.Slot, Index: -1}
				if a.Index != nil {
					m.Index = *a.Index
				}
				return ev(EventNodeMoved, m), nil
			},
		},
		{
			id: "prefab.removeNode", title: "Remove a node", events: []string{EventNodeRemoved},
			description: "Removes the node at path with its subtree. Removing the root empties the tree.",
			args:        `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
			example:     `{"path":"root/due"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Path string }](c)
				if problems != nil {
					return nil, problems
				}
				if _, problems := c.node(a.Path, "args.path"); problems != nil {
					return nil, problems
				}
				return ev(EventNodeRemoved, map[string]any{"path": a.Path}), nil
			},
		},
		{
			id: "prefab.wrapNodes", title: "Wrap nodes", events: []string{EventNodesWrapped},
			description: "Puts sibling nodes, in their order, into a new wrapper node (such as a mui.stack), which takes the first one's place. The wrapper is given without children; its key is made from its type when left out.",
			args:        `{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"},"minItems":1},"wrapper":` + nodeSchema + `},"required":["paths","wrapper"]}`,
			example:     `{"paths":["root/title","root/due"],"wrapper":{"type":"mui.stack","props":{"direction":"column"}}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[Wrap](c)
				if problems != nil {
					return nil, problems
				}
				if a.Wrapper == nil || a.Wrapper.Type == "" {
					return nil, problem("args.wrapper.type", "a wrapper needs a type", "use a container such as mui.stack or mui.box")
				}
				if len(a.Wrapper.Children) > 0 {
					return nil, problem("args.wrapper.children", "the wrapper's children are the wrapped nodes", "give the wrapper without children")
				}
				var first place
				for i, path := range a.Paths {
					at, problems := c.node(path, fmt.Sprintf("args.paths.%d", i))
					if problems != nil {
						return nil, problems
					}
					if at.parent == nil && len(a.Paths) > 1 {
						return nil, problem(fmt.Sprintf("args.paths.%d", i), "the root has no siblings to wrap with", "wrap the root alone")
					}
					if i == 0 {
						first = at
					} else if at.parent != first.parent || at.slot != first.slot {
						return nil, problem(fmt.Sprintf("args.paths.%d", i), "only siblings can be wrapped together", "wrap nodes with one parent")
					}
				}
				keyed(a.Wrapper, keys(c.prefab().Template))
				return ev(EventNodesWrapped, a), nil
			},
		},
		{
			id: "prefab.unwrapNode", title: "Unwrap a node", events: []string{EventNodeUnwrapped},
			description: "Replaces the node at path with its children, in its place. The root can be unwrapped when it has one child.",
			args:        `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
			example:     `{"path":"root/texts"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Path string }](c)
				if problems != nil {
					return nil, problems
				}
				at, problems := c.node(a.Path, "args.path")
				if problems != nil {
					return nil, problems
				}
				if len(at.node.Slots) > 0 {
					return nil, problem("args.path", "the node has content in slots, which would be lost", "move it out first")
				}
				if at.parent == nil && len(at.node.Children) != 1 {
					return nil, problem("args.path", "the root can be unwrapped only when it has one child", "")
				}
				return ev(EventNodeUnwrapped, map[string]any{"path": a.Path}), nil
			},
		},
		{
			id: "prefab.attachGraph", title: "Attach a graph", events: []string{EventGraphAttached},
			description: "Attaches a graph to a prefab under a name its tree's handlers use (on: {change: {handler: \"onToggle\"}}): a ui.handler graph for an event, or a prefab.logic graph for its lifecycle. Make the graph first with bp.create.",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"graph":{"type":"string","description":"The graph's id"}},"required":["name","graph"]}`,
			example:     `{"name":"onToggle","graph":"taskRow-onToggle"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Name, Graph string }](c)
				if problems != nil {
					return nil, problems
				}
				if c.prefab().Graphs[a.Name] == a.Graph {
					return nil, nil
				}
				return ev(EventGraphAttached, map[string]any{"name": a.Name, "graph": a.Graph}), nil
			},
		},
		{
			id: "prefab.detachGraph", title: "Detach a graph", events: []string{EventGraphDetached},
			description: "Detaches a graph from a prefab; the graph itself stays. A handler in the tree still naming it must be changed first.",
			args:        `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
			example:     `{"name":"logic"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Name string }](c)
				if problems != nil {
					return nil, problems
				}
				if _, ok := c.prefab().Graphs[a.Name]; !ok {
					return nil, nil
				}
				return ev(EventGraphDetached, map[string]any{"name": a.Name}), nil
			},
		},
		{
			id: "prefab.setPreview", title: "Set preview data", events: []string{EventPreviewSet},
			description: "Sets the sample props the Prefab Editor renders a prefab with. They are part of the draft, not of its interface.",
			args:        `{"type":"object","properties":{"props":{"type":"object"}},"required":["props"]}`,
			example:     `{"props":{"task":{"_id":"1","title":"Water the plants","done":false}}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Props map[string]any }](c)
				if problems != nil {
					return nil, problems
				}
				if equal(a.Props, c.prefab().Preview.Props) {
					return nil, nil
				}
				return ev(EventPreviewSet, map[string]any{"props": a.Props}), nil
			},
		},
		{
			id: "prefab.setVariants", title: "Set a prefab's variants", events: []string{EventVariantsSet},
			description: "Sets a prefab's variants, replacing them all: named presets of its props (compact: {props: {dense: true}}) that an instance picks with its node's variant, its own props overriding the variant's. Removing a variant that published prefabs use breaks them, which prefab.publish reports.",
			args:        `{"type":"object","properties":{"variants":{"type":"object","description":"name → {description?, props: {prop: value}}","additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"props":{"type":"object"}},"required":["props"]}}},"required":["variants"]}`,
			example:     `{"variants":{"compact":{"description":"One line, small text","props":{"dense":true}}}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct{ Variants map[string]ui.VariantDecl }](c)
				if problems != nil {
					return nil, problems
				}
				for k, v := range a.Variants {
					if v.Props == nil {
						v.Props = map[string]any{}
						a.Variants[k] = v
					}
				}
				if equal(a.Variants, c.prefab().Variants) || len(a.Variants)+len(c.prefab().Variants) == 0 {
					return nil, nil
				}
				return ev(EventVariantsSet, map[string]any{"variants": a.Variants}), nil
			},
		},
		{
			id: "prefab.delete", title: "Delete a prefab", events: []string{EventDeleted},
			description: "Deletes a prefab: it is hidden, not erased, and prefab.restore brings it back. Its published versions keep rendering where they are pinned. Refused while a published prefab or the last published version of an app uses it.",
			args:        `{"type":"object","properties":{}}`,
			example:     `{}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if uses := c.users(); len(uses) > 0 {
					return nil, problem("target", fmt.Sprintf("the prefab %q is used by %s", c.Target.ID, strings.Join(uses, ", ")), "remove it from those first, and publish them")
				}
				return ev(EventDeleted, map[string]any{}), nil
			},
		},
		{
			id: "prefab.restore", title: "Restore a prefab", events: []string{EventRestored},
			description: "Brings back a deleted prefab as it was.",
			args:        `{"type":"object","properties":{}}`,
			example:     `{}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if !c.draft.Deleted {
					return nil, problem("target", fmt.Sprintf("the prefab %q is not deleted", c.Target.ID), "")
				}
				return ev(EventRestored, map[string]any{}), nil
			},
		},
		{
			id: "prefab.publish", title: "Publish a prefab", events: []string{EventPublished}, action: "prefabs.publish",
			description: "Publishes the draft as a new version, which its instances follow; it is also kept in the tenant database. rev is the draft's rev the publisher last saw: a change made since is not published unseen. Removing a prop, event, slot or variant or changing a prop's type since the last version is refused when published prefabs use it, listing them ({change, usedBy: [{prefab, path}]}), unless allowBreaking is true.",
			args:        `{"type":"object","properties":{"rev":{"type":"integer","description":"The draft's rev as last seen (prefab.* results and prefab.describe give it)"},"allowBreaking":{"type":"boolean","description":"Publish although published prefabs using it break"}},"required":["rev"]}`,
			example:     `{"rev":7}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := args[struct {
					Rev           int
					AllowBreaking bool
				}](c)
				if problems != nil {
					return nil, problems
				}
				if a.Rev != c.draft.Rev {
					return nil, problem("args.rev", fmt.Sprintf("the draft changed since rev %d: it is at rev %d", a.Rev, c.draft.Rev), "look at the changes, then publish rev "+strconv.Itoa(c.draft.Rev))
				}
				p := *c.prefab()
				var breaking []Break
				if prev, ok := c.published(c.Target.ID); ok {
					breaking = c.usedBy(Breaking(prev, p))
				}
				if len(breaking) > 0 && !a.AllowBreaking {
					var out []live.Problem
					for _, b := range breaking {
						var uses []string
						for _, u := range b.UsedBy {
							uses = append(uses, u.Prefab+" at "+u.Path)
						}
						out = append(out, live.Problem{Code: "prefab_breaking", Path: "prefab." + b.Path,
							Message: fmt.Sprintf("%s breaks %s", b.Change, strings.Join(uses, ", ")),
							Fix:     "change those prefabs first, or publish with allowBreaking: true"})
					}
					return nil, out
				}
				// The copy into the tenant database runs before the event lands
				if err := Follow(c.ServiceArgs, c.deps); err != nil {
					return nil, problem("", "the published versions cannot be kept: "+err.Error(), "")
				}
				p.Version = c.draft.Published + 1
				return ev(EventPublished, Publication{Version: p.Version, Prefab: p, Breaking: breaking}), nil
			},
		},
	}
}

func equal(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
