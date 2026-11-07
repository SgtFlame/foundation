package apps

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// DocumentFormat names the export document's format and its version.
const DocumentFormat = "forge.app/1"

// Document is a published app version exported as one JSON document
// (forge 05e): its manifest, every part it uses with every reference
// resolved inside the document, the primitives those parts use, and,
// when asked for, sample records. app.import recreates it in another
// tenant.
type Document struct {
	Format      string    `json:"format"`
	App         string    `json:"app"`
	Version     int       `json:"version"` // the version exported
	PublishedAt time.Time `json:"publishedAt"`
	ExportedAt  time.Time `json:"exportedAt"`
	Manifest    Manifest  `json:"manifest"`
	// Parts are the app's own (owner app:<id>).
	Parts Parts `json:"parts"`
	// TenantParts are the tenant's parts the app uses (a tenant-library
	// prefab, a shared entity type, a graph of no app), copied so a tenant
	// without them gets them (forge 05e question 2). Import asks before
	// overwriting one the target has.
	TenantParts Parts `json:"tenantParts"`
	// AppParts are parts of the apps it requires: never imported, the
	// target must have them.
	AppParts Parts `json:"appParts"`
	// Primitives are what the server provides that the parts use.
	Primitives Primitives `json:"primitives"`
	Seed       Seed       `json:"seed,omitempty"`
}

// Parts are a document's prefabs, graphs and entity types, each as it
// was published.
type Parts struct {
	Prefabs  []PrefabPart `json:"prefabs"`
	Graphs   []GraphPart  `json:"graphs"`
	Entities []EntityPart `json:"entities"`
}

// PrefabPart is a published prefab version in a document.
type PrefabPart struct {
	ID          string    `json:"id"`
	Version     int       `json:"version"`
	PublishedAt time.Time `json:"publishedAt,omitzero"`
	Prefab      ui.Prefab `json:"prefab"`
}

// GraphPart is a graph as the app version copied it.
type GraphPart struct {
	ID          string          `json:"id"`
	Version     int             `json:"version"`
	PublishedAt time.Time       `json:"publishedAt,omitzero"` // the app version's: graphs keep no publication times
	Graph       cognition.Graph `json:"graph"`
}

// EntityPart is a published entity type version in a document.
type EntityPart struct {
	ID          string             `json:"id"`
	Version     int                `json:"version"`
	PublishedAt time.Time          `json:"publishedAt,omitzero"`
	Entity      records.EntityType `json:"entity"`
}

// Primitives are a document's references to what the server provides:
// components by id, node types by id and built-in graphs. Components and
// nodes carry no versions yet (forge 05a decision 6).
type Primitives struct {
	Components []string `json:"components"`
	Nodes      []string `json:"nodes"`
	Graphs     []string `json:"graphs"`
}

// Seed are sample records by entity type (forge iteration question 3).
type Seed map[string][]SeedRecord

// SeedRecord is one sample record: its id, kept so relations between
// them hold, and its values by field id.
type SeedRecord struct {
	ID     string                     `json:"id"`
	Values map[string]json.RawMessage `json:"values"`
}

// Part types, in problem paths and in an import's decisions
// (prefab:recordTable).
const (
	PartPrefab = "prefab"
	PartGraph  = "graph"
	PartEntity = "entity"
)

// PartKey names a part in an import's decisions.
func PartKey(typ, id string) string { return typ + ":" + id }

// Normalize gives a document's lists empty slices rather than null.
func (d *Document) Normalize() {
	Normalize(&d.Manifest)
	for _, p := range []*Parts{&d.Parts, &d.TenantParts, &d.AppParts} {
		if p.Prefabs == nil {
			p.Prefabs = []PrefabPart{}
		}
		if p.Graphs == nil {
			p.Graphs = []GraphPart{}
		}
		if p.Entities == nil {
			p.Entities = []EntityPart{}
		}
	}
	for _, l := range []*[]string{&d.Primitives.Components, &d.Primitives.Nodes, &d.Primitives.Graphs} {
		if *l == nil {
			*l = []string{}
		}
	}
}

// DocumentEnv is what a document is checked against: the server's
// primitives.
type DocumentEnv struct {
	Catalog map[string]ui.Component // components
	Node    func(typ string) bool   // a node type exists
	// Builtin is a built-in graph of the server.
	Builtin func(id string) (cognition.Graph, bool)
	Query   func(name string) bool // a live query of the server
}

// CheckDocument is validation of a whole document plus the
// blueprints-only check (forge 05e; forge Architecture rule 1): the
// document references only primitives, by id, and its own parts. Every
// problem is at a path such as parts.prefab[myTasks].template.root.type
// or manifest.pages[/mine].prefab.
func CheckDocument(d Document, env DocumentEnv) []live.Problem {
	c := &docCheck{d: d, env: env, prefabs: map[string]ui.Prefab{}, graphs: map[string]cognition.Graph{}, entities: map[string]records.EntityType{}}
	if d.Format != DocumentFormat {
		c.add("format", fmt.Sprintf("the format is %q, not %s", d.Format, DocumentFormat), "export the app again with app.export")
	}
	if d.Manifest.ID != d.App {
		c.add("manifest.id", fmt.Sprintf("the manifest is %q, the document's app %q", d.Manifest.ID, d.App), "export the app again with app.export")
	}
	c.index()
	c.listed()
	for _, g := range groups {
		for _, p := range g.of(d).Prefabs {
			c.prefab(g.name+".prefab["+p.ID+"]", p.Prefab, g.name == "appParts")
		}
		for _, p := range g.of(d).Graphs {
			c.graph(g.name+".graph["+p.ID+"]", p.Graph)
		}
		for _, p := range g.of(d).Entities {
			c.entity(g.name+".entity["+p.ID+"]", p.Entity)
		}
	}
	menv := Env{Catalog: env.Catalog,
		Prefab: func(id string) (ui.Prefab, bool) { p, ok := c.prefabs[id]; return p, ok },
		Graph:  c.graphOf,
		Entity: func(id string) (records.EntityType, bool) { e, ok := c.entities[id]; return e, ok },
	}
	for _, p := range CheckManifest(d.Manifest, menv) {
		p.Path = strings.TrimSuffix("manifest."+p.Path, ".")
		c.problems = append(c.problems, p)
	}
	c.seed()
	slices.SortStableFunc(c.problems, func(a, b live.Problem) int { return strings.Compare(a.Path, b.Path) })
	return c.problems
}

type docCheck struct {
	d        Document
	env      DocumentEnv
	problems []live.Problem
	prefabs  map[string]ui.Prefab
	graphs   map[string]cognition.Graph
	entities map[string]records.EntityType
	used     struct{ components, nodes, graphs map[string]bool }
}

func (c *docCheck) add(path, message, fix string) {
	c.problems = append(c.problems, live.Problem{Code: "app_document", Path: path, Message: message, Fix: fix})
}

// group is one of a document's groups of parts.
type group struct {
	name string
	of   func(d Document) Parts
}

var groups = []group{
	{"parts", func(d Document) Parts { return d.Parts }},
	{"tenantParts", func(d Document) Parts { return d.TenantParts }},
	{"appParts", func(d Document) Parts { return d.AppParts }},
}

// index collects the parts by id and checks each is where its owner says.
func (c *docCheck) index() {
	own := Owner(c.d.App)
	where := func(path, owner, group string) {
		switch {
		case group == "parts" && owner != own:
			c.add(path+".owner", fmt.Sprintf("a part of the app has the owner %q, not %s", owner, own), "move it to tenantParts or appParts")
		case group == "tenantParts" && strings.HasPrefix(owner, "app:"):
			c.add(path+".owner", fmt.Sprintf("a tenant part has the owner %q", owner), "move it to parts or appParts")
		case group == "appParts":
			app, _ := strings.CutPrefix(owner, "app:")
			if !strings.HasPrefix(owner, "app:") || app == c.d.App || !slices.Contains(c.d.Manifest.Requires.Apps, app) {
				c.add(path+".owner", fmt.Sprintf("the part's owner %q is not an app this one requires", owner), "add the app to the manifest's requires.apps, or move the part")
			}
		}
	}
	dup := func(path string, seen bool) {
		if seen {
			c.add(path, "the document has this part twice", "keep one")
		}
	}
	for _, g := range groups {
		ps := g.of(c.d)
		for _, p := range ps.Prefabs {
			path := g.name + ".prefab[" + p.ID + "]"
			_, seen := c.prefabs[p.ID]
			dup(path, seen)
			if p.Prefab.ID != p.ID {
				c.add(path+".prefab.id", fmt.Sprintf("the definition is %q", p.Prefab.ID), "give the part its definition's id")
			}
			where(path, p.Prefab.Owner, g.name)
			c.prefabs[p.ID] = p.Prefab
		}
		for _, p := range ps.Graphs {
			path := g.name + ".graph[" + p.ID + "]"
			_, seen := c.graphs[p.ID]
			dup(path, seen)
			if p.Graph.ID != p.ID {
				c.add(path+".graph.id", fmt.Sprintf("the definition is %q", p.Graph.ID), "give the part its definition's id")
			}
			where(path, p.Graph.Owner, g.name)
			c.graphs[p.ID] = p.Graph
		}
		for _, p := range ps.Entities {
			path := g.name + ".entity[" + p.ID + "]"
			_, seen := c.entities[p.ID]
			dup(path, seen)
			if p.Entity.ID != p.ID {
				c.add(path+".entity.id", fmt.Sprintf("the definition is %q", p.Entity.ID), "give the part its definition's id")
			}
			where(path, p.Entity.Owner, g.name)
			c.entities[p.ID] = p.Entity
		}
	}
	for _, id := range c.d.Primitives.Graphs {
		if _, ok := c.graphs[id]; ok {
			c.add("primitives.graphs["+id+"]", "a built-in graph is also a part", "keep one")
		}
	}
}

// listed checks the primitives the document names exist here.
func (c *docCheck) listed() {
	c.used.components, c.used.nodes, c.used.graphs = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, id := range c.d.Primitives.Components {
		if _, ok := c.env.Catalog[id]; !ok {
			c.add("primitives.components["+id+"]", fmt.Sprintf("this server has no component %s", id), "install a server version that has it")
		}
	}
	for _, id := range c.d.Primitives.Nodes {
		if c.env.Node != nil && !c.env.Node(id) {
			c.add("primitives.nodes["+id+"]", fmt.Sprintf("this server has no node type %s", id), "install a server version that has it")
		}
	}
	for _, id := range c.d.Primitives.Graphs {
		if _, ok := c.builtin(id); !ok {
			c.add("primitives.graphs["+id+"]", fmt.Sprintf("this server has no built-in graph %s", id), "install a server version that has it")
		}
	}
}

func (c *docCheck) builtin(id string) (cognition.Graph, bool) {
	if c.env.Builtin == nil {
		return cognition.Graph{}, false
	}
	return c.env.Builtin(id)
}

// graphOf is a graph the document has or names as built in.
func (c *docCheck) graphOf(id string) (cognition.Graph, bool) {
	if g, ok := c.graphs[id]; ok {
		return g, true
	}
	if slices.Contains(c.d.Primitives.Graphs, id) {
		return c.builtin(id)
	}
	return cognition.Graph{}, false
}

// entityRef checks a reference to an entity type.
func (c *docCheck) entityRef(path, id string) {
	if id == "" || id == records.UserEntity {
		return
	}
	if _, ok := c.entities[id]; !ok {
		c.add(path, fmt.Sprintf("the entity type %s is not in the document", id), "export the app again: every entity type its parts name must be one of its own, a tenant's, or a required app's")
	}
}

// prefab checks a prefab with every reference inside the document; a
// required app's prefab is checked only for being complete.
func (c *docCheck) prefab(path string, p ui.Prefab, other bool) {
	env := ui.PrefabEnv{
		Catalog: c.env.Catalog,
		Prefab:  func(id string) (ui.Prefab, bool) { p, ok := c.prefabs[id]; return p, ok },
		Uses: func(id string) []string {
			return ui.PrefabsUsed(c.prefabs[id].Template)
		},
		Entity: func(id string) bool { _, ok := c.entities[id]; return ok || id == records.UserEntity },
		Query: func(name string) bool {
			if slices.ContainsFunc(c.d.Manifest.Queries, func(q Query) bool { return q.Name == name }) || (c.env.Query != nil && c.env.Query(name)) {
				return true
			}
			_, ok := c.graphOf("qry-" + name)
			return ok
		},
		Graph: func(id string) (string, bool) {
			g, ok := c.graphOf(id)
			return g.Kind, ok
		},
	}
	for _, pr := range ui.CheckPrefab(p, env) {
		pr.Path = path + ".prefab." + pr.Path
		c.problems = append(c.problems, pr)
	}
	if other {
		return
	}
	ui.WalkTemplate(p.Template, func(keys []string, n *ui.Node) {
		at := path + ".template." + strings.Join(keys, "/")
		switch id, version, inst := ui.InstanceOf(n.Type); {
		case inst && version != 0:
			c.add(at+".type", fmt.Sprintf("the instance pins version %d of %s, which another tenant numbers anew", version, id), "use prefab."+id+" without a version")
		case inst, n.Type == ui.SlotType, n.Type == ui.RepeatType, n.Type == ui.WhenType:
		default:
			c.used.components[n.Type] = true
			if !slices.Contains(c.d.Primitives.Components, n.Type) {
				c.add(at+".type", fmt.Sprintf("the component %s is not among the document's primitives", n.Type), "export the app again with app.export")
			}
		}
		if e, ok := n.Props["entity"].(string); ok {
			c.entityRef(at+".props.entity", e)
		}
	})
	for _, name := range slices.Sorted(maps.Keys(p.Graphs)) {
		c.graphRef(path+".prefab.graphs."+name, p.Graphs[name])
	}
}

func (c *docCheck) graphRef(path, id string) {
	if _, ok := c.graphOf(id); !ok {
		c.add(path, fmt.Sprintf("the graph %s is not in the document", id), "export the app again: every graph its parts name must be in it, or built in")
	}
}

// graph checks a graph's node types are primitives and the entity types
// its nodes name are in the document.
func (c *docCheck) graph(path string, g cognition.Graph) {
	for i, n := range g.Nodes {
		at := fmt.Sprintf("%s.graph.nodes.%d", path, i)
		c.used.nodes[n.Type] = true
		if !slices.Contains(c.d.Primitives.Nodes, n.Type) {
			c.add(at+".type", fmt.Sprintf("the node type %s is not among the document's primitives", n.Type), "export the app again with app.export")
		}
		var config struct {
			Entity any `json:"entity"`
		}
		if json.Unmarshal(n.Config, &config) == nil {
			if e, ok := config.Entity.(string); ok {
				c.entityRef(at+".config.entity", e)
			}
		}
	}
}

// entity checks the entity types an entity type relates to are in the
// document.
func (c *docCheck) entity(path string, e records.EntityType) {
	for i, f := range e.Fields {
		if f.Relation != nil {
			c.entityRef(fmt.Sprintf("%s.entity.fields.%d.relation.entity", path, i), f.Relation.Entity)
		}
	}
}

// seed checks sample records are of the app's own entity types.
func (c *docCheck) seed() {
	for _, entity := range slices.Sorted(maps.Keys(c.d.Seed)) {
		if !slices.ContainsFunc(c.d.Parts.Entities, func(p EntityPart) bool { return p.ID == entity }) {
			c.add("seed."+entity, fmt.Sprintf("sample records of %s, which is not one of the app's own entity types", entity), "leave them out")
		}
		for i, r := range c.d.Seed[entity] {
			if !live.ValidToken(r.ID) {
				c.add(fmt.Sprintf("seed.%s.%d.id", entity, i), fmt.Sprintf("%q is not a record id", r.ID), "give each record its id")
			}
		}
	}
}

// Used are the primitives a document's parts use, for an export to list.
func Used(d Document, env DocumentEnv) Primitives {
	c := &docCheck{d: d, env: env, prefabs: map[string]ui.Prefab{}, graphs: map[string]cognition.Graph{}, entities: map[string]records.EntityType{}}
	c.d.Primitives = Primitives{}
	c.index()
	c.listed()
	for _, g := range groups[:2] {
		for _, p := range g.of(d).Prefabs {
			c.prefab("", p.Prefab, false)
		}
		for _, p := range g.of(d).Graphs {
			c.graph("", p.Graph)
		}
	}
	return Primitives{Components: slices.Sorted(maps.Keys(c.used.components)), Nodes: slices.Sorted(maps.Keys(c.used.nodes)), Graphs: []string{}}
}
