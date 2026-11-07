package apps

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Env is what a manifest is checked against: the tenant's prefabs, graphs
// and entity types (drafts or published), and the primitive catalog. A
// nil function finds nothing.
type Env struct {
	Prefab  func(id string) (ui.Prefab, bool)
	Graph   func(id string) (cognition.Graph, bool) // its kind and owner
	Entity  func(id string) (records.EntityType, bool)
	Catalog map[string]ui.Component
}

// dotted is a command type or query name: tokens joined with '.'.
var dotted = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

// ValidName reports whether s can be a command type or a query name.
func ValidName(s string) bool { return dotted.MatchString(s) }

// RouteParams are a route's params (/task/:id gives id), or an error when
// it is not a route.
func RouteParams(route string) ([]string, error) {
	if !strings.HasPrefix(route, "/") {
		return nil, fmt.Errorf("a route starts with /")
	}
	var params []string
	if route == "/" {
		return params, nil
	}
	for seg := range strings.SplitSeq(route[1:], "/") {
		name, param := strings.CutPrefix(seg, ":")
		if !live.ValidToken(name) {
			return nil, fmt.Errorf("%q is not a route segment: use letters, digits, _ and -, or :param", seg)
		}
		if param {
			if slices.Contains(params, name) {
				return nil, fmt.Errorf("the param %s is used twice", name)
			}
			params = append(params, name)
		}
	}
	return params, nil
}

// CheckManifest reports what is wrong with a manifest, at paths like
// pages[/mine].prefab, each with a fix a model can act on. Drafts are
// checked by it after every app.* command, and publishing runs it again.
func CheckManifest(m Manifest, env Env) []live.Problem {
	c := &check{m: m, env: env}
	c.info()
	c.pages()
	c.nav()
	c.editorKinds()
	c.entities()
	c.commands()
	c.queries()
	c.roles()
	c.settings()
	c.triggers()
	c.requires()
	return c.problems
}

type check struct {
	m        Manifest
	env      Env
	problems []live.Problem
	routes   map[string][]string // each page's route → its params
}

func (c *check) add(path, message, fix string) {
	c.problems = append(c.problems, live.Problem{Path: path, Message: message, Fix: fix})
}

// owned checks the owner of a part the app uses: its own, the tenant's,
// or an app it requires.
func (c *check) owned(path, what, owner string) {
	app, ok := strings.CutPrefix(owner, "app:")
	if !ok || app == c.m.ID || slices.Contains(c.m.Requires.Apps, app) {
		return
	}
	c.add(path, fmt.Sprintf("%s belongs to the app %s", what, app), fmt.Sprintf("add %s to requires.apps with app.setInfo, or use a part of this app's own", app))
}

func (c *check) info() {
	if !live.ValidToken(c.m.ID) {
		c.add("id", fmt.Sprintf("%q is not an app id", c.m.ID), "use letters, digits, _ and -")
	}
	if c.m.ID+"." == DraftPrefix {
		c.add("id", fmt.Sprintf("%q is kept for apps' draft runs", c.m.ID), "choose another id")
	}
	if strings.TrimSpace(c.m.Name) == "" {
		c.add("name", "an app needs a name", "set one with app.setInfo {name}")
	}
	if c.m.Frame != "" {
		id, ok := strings.CutPrefix(c.m.Frame, "prefab.")
		switch {
		case !ok:
			c.add("frame", fmt.Sprintf("%q is not a prefab", c.m.Frame), "use prefab.<id>, or leave it empty for the default frame")
		default:
			if p, ok := c.prefab(id); !ok {
				c.add("frame", fmt.Sprintf("there is no prefab %q", id), "make it first, or leave the frame empty for the default")
			} else {
				c.owned("frame", "the prefab "+id, p.Owner)
				if _, ok := p.Slots[FrameSlot]; !ok {
					c.add("frame", fmt.Sprintf("the prefab %s has no %s slot for the pages", id, FrameSlot), "declare a page slot with prefab.setInterface, where the current page goes")
				}
			}
		}
	}
}

func (c *check) prefab(id string) (ui.Prefab, bool) {
	if c.env.Prefab == nil || !live.ValidToken(id) {
		return ui.Prefab{}, false
	}
	return c.env.Prefab(id)
}

// graph checks a command's or query's graph: there, of its kind, and
// defining the name the manifest gives it (its spec's command or query).
func (c *check) graph(path, id, kind, field, name string) {
	var g cognition.Graph
	ok := false
	if c.env.Graph != nil && live.ValidToken(id) {
		g, ok = c.env.Graph(id)
	}
	switch {
	case id == "":
		c.add(path, "a graph is needed", fmt.Sprintf("make a %s graph with bp.create and name it here", kind))
	case !ok:
		c.add(path, fmt.Sprintf("there is no graph %q", id), fmt.Sprintf("make a %s graph with bp.create first", kind))
	case g.Kind != kind:
		c.add(path, fmt.Sprintf("the graph %s is a %s graph, not a %s graph", id, g.Kind, kind), fmt.Sprintf("name a %s graph", kind))
	default:
		c.owned(path, "the graph "+id, g.Owner)
		var spec map[string]any
		_ = json.Unmarshal(g.Spec, &spec)
		if got, _ := spec[field].(string); got != name {
			c.add(path, fmt.Sprintf("the graph %s defines the %s %q, not %q", id, field, got, name), fmt.Sprintf("name the graph whose spec's %s is %s", field, name))
		}
	}
}

func (c *check) pages() {
	c.routes = map[string][]string{}
	for _, p := range c.m.Pages {
		path := "pages[" + p.Route + "]"
		params, err := RouteParams(p.Route)
		if err != nil {
			c.add(path+".route", fmt.Sprintf("%q is not a route: %v", p.Route, err), "use a path such as /mine or /task/:id")
			continue
		}
		if _, dup := c.routes[p.Route]; dup {
			c.add(path+".route", fmt.Sprintf("the route %s is used twice", p.Route), "give each page its own route")
			continue
		}
		c.routes[p.Route] = params
		pf, ok := c.prefab(p.Prefab)
		if !ok {
			c.add(path+".prefab", fmt.Sprintf("there is no prefab %q", p.Prefab), "make it with prefab.create first (prefab.list lists them)")
			continue
		}
		c.owned(path+".prefab", "the prefab "+p.Prefab, pf.Owner)
		// The route's params are the prefab's props
		for _, name := range params {
			if _, ok := pf.Props[name]; !ok {
				c.add(path+".route", fmt.Sprintf("the prefab %s has no prop %s for the route's :%s", p.Prefab, name, name), "declare the prop with prefab.setInterface, or rename the param")
			}
		}
		for name, d := range pf.Props {
			if d.Required && d.Default == nil && !slices.Contains(params, name) {
				c.add(path+".prefab", fmt.Sprintf("the prefab %s needs the prop %s, which the route does not give", p.Prefab, name), fmt.Sprintf("add :%s to the route, or give the prop a default", name))
			}
		}
	}
	if c.m.Home != "" {
		c.plainRoute("home", c.m.Home)
	}
}

// plainRoute checks that route is a page's route without params.
func (c *check) plainRoute(path, route string) {
	params, ok := c.routes[route]
	switch {
	case !ok:
		c.add(path, fmt.Sprintf("there is no page at %s", route), "use one of the pages' routes: "+c.routeList())
	case len(params) > 0:
		c.add(path, fmt.Sprintf("the route %s needs params", route), "use a route without :params")
	}
}

func (c *check) routeList() string {
	if len(c.m.Pages) == 0 {
		return "(none yet: add a page with app.addPage)"
	}
	var out []string
	for _, p := range c.m.Pages {
		out = append(out, p.Route)
	}
	return strings.Join(out, ", ")
}

func (c *check) nav() {
	for i, n := range c.m.Nav {
		path := fmt.Sprintf("nav[%d]", i)
		if strings.TrimSpace(n.Title) == "" {
			c.add(path+".title", "a navigation item needs a title", "")
		}
		c.plainRoute(path+".route", n.Route)
	}
}

func (c *check) editorKinds() {
	seen := map[string]bool{}
	for _, k := range c.m.EditorKinds {
		path := "editorKinds[" + k.Kind + "]"
		switch {
		case !live.ValidToken(k.Kind) || k.Kind == "main":
			c.add(path+".kind", fmt.Sprintf("%q is not an editor kind", k.Kind), "use letters, digits, _ and - (main is the app's own tab)")
		case seen[k.Kind]:
			c.add(path+".kind", fmt.Sprintf("the editor kind %s is declared twice", k.Kind), "")
		}
		seen[k.Kind] = true
		if _, ok := c.routes[k.Page]; !ok {
			c.add(path+".page", fmt.Sprintf("there is no page at %s", k.Page), "use one of the pages' routes: "+c.routeList())
		}
		if strings.TrimSpace(k.Title) == "" {
			c.add(path+".title", "an editor kind needs a title", "a template over the page's data, such as {record.title}")
		}
	}
}

func (c *check) entities() {
	seen := map[string]bool{}
	for _, id := range c.m.Entities {
		path := "entities[" + id + "]"
		if seen[id] {
			c.add(path, fmt.Sprintf("the entity type %s is listed twice", id), "")
			continue
		}
		seen[id] = true
		var e records.EntityType
		ok := false
		if c.env.Entity != nil && live.ValidToken(id) {
			e, ok = c.env.Entity(id)
		}
		switch {
		case !ok:
			c.add(path, fmt.Sprintf("there is no entity type %q", id), "make it with entity.create first (entity.list lists them)")
		case e.Hidden:
			c.add(path, fmt.Sprintf("the entity type %s is deleted", id), "restore it with entity.restore, or remove it from the app")
		default:
			c.owned(path, "the entity type "+id, e.Owner)
		}
	}
}

// action checks an action named by a command or a role:
// entity:<id>.<read|create|update|delete|*> on one of the app's own
// entity types (forge 05c question 1: its roles reach nothing beyond it).
func (c *check) action(path, a string) {
	entity, _, ok := EntityAction(a)
	if !ok {
		c.add(path, fmt.Sprintf("%q is not an action", a), "use entity:<id>.<action>, the action read, create, update, delete or *, such as entity:task.update or entity:task.*")
		return
	}
	if !slices.Contains(c.m.Entities, entity) {
		c.add(path, fmt.Sprintf("the app does not use the entity type %s", entity), "add it with app.addEntity, or name one the app uses")
		return
	}
	if c.env.Entity != nil {
		if e, ok := c.env.Entity(entity); ok && e.Owner != Owner(c.m.ID) {
			c.add(path, fmt.Sprintf("the entity type %s is not the app's own, so the app's roles cannot reach it", entity), "use one of the app's own entity types; the tenant's roles give access to the others")
		}
	}
}

func (c *check) commands() {
	seen := map[string]bool{}
	for _, cmd := range c.m.Commands {
		path := "commands[" + cmd.Type + "]"
		switch {
		case !ValidName(cmd.Type):
			c.add(path+".type", fmt.Sprintf("%q is not a command type", cmd.Type), "use dotted names, such as task.reassign")
		case seen[cmd.Type]:
			c.add(path+".type", fmt.Sprintf("the command %s is declared twice", cmd.Type), "")
		}
		seen[cmd.Type] = true
		c.graph(path+".graph", cmd.Graph, "command", "command", cmd.Type)
		if _, acts, ok := EntityAction(cmd.Action); ok && len(acts) > 1 {
			c.add(path+".action", fmt.Sprintf("a command needs one action, and %s is every one", cmd.Action), "name the one it does, such as entity:task.update")
		} else if cmd.Action != "" {
			c.action(path+".action", cmd.Action)
		}
	}
}

func (c *check) queries() {
	seen := map[string]bool{}
	for _, q := range c.m.Queries {
		path := "queries[" + q.Name + "]"
		switch {
		case !ValidName(q.Name):
			c.add(path+".name", fmt.Sprintf("%q is not a query name", q.Name), "use dotted names, such as myTasks")
		case seen[q.Name]:
			c.add(path+".name", fmt.Sprintf("the query %s is declared twice", q.Name), "")
		}
		seen[q.Name] = true
		c.graph(path+".graph", q.Graph, "data.query", "query", q.Name)
	}
}

func (c *check) roles() {
	seen := map[string]bool{}
	installer := ""
	for _, r := range c.m.Roles {
		path := "roles[" + r.ID + "]"
		if r.Installer {
			if installer != "" {
				c.add(path+".installer", fmt.Sprintf("the role %s is marked as the installer's already", installer), "mark one role only, with installer: false on the others")
			}
			installer = r.ID
		}
		switch {
		case !live.ValidToken(r.ID):
			c.add(path+".id", fmt.Sprintf("%q is not a role id", r.ID), "use letters, digits, _ and -")
		case seen[r.ID]:
			c.add(path+".id", fmt.Sprintf("the role %s is declared twice", r.ID), "")
		}
		seen[r.ID] = true
		for i, a := range r.Actions {
			c.action(fmt.Sprintf("%s.actions[%d]", path, i), a)
		}
		for entity, rule := range r.RowRules {
			rp := path + ".rowRules." + entity
			if !slices.Contains(c.m.Entities, entity) {
				c.add(rp, fmt.Sprintf("the app does not use the entity type %s", entity), "add it with app.addEntity, or remove the rule")
			}
			if _, err := records.CompileRule(rule); err != nil {
				c.add(rp, fmt.Sprintf("the rule does not compile: %v", err), "an expression over record and principal giving a bool, such as record.assignee == principal.user")
			}
		}
	}
}

func (c *check) settings() {
	s := c.m.Settings
	if s.Schema == nil {
		if len(s.Defaults) > 0 {
			c.add("settings.defaults", "there are defaults but no schema", "set the schema with app.setSettingsSchema")
		}
		return
	}
	if s.Schema["type"] != "object" {
		c.add("settings.schema.type", "the settings' schema is an object's", `give it "type": "object" and its properties`)
		return
	}
	schema, _ := json.Marshal(s.Schema)
	defaults, _ := json.Marshal(s.Defaults)
	if s.Defaults == nil {
		defaults = []byte("{}")
	}
	if err := tools.CheckArgs(schema, defaults); err != nil {
		c.add("settings.defaults", fmt.Sprintf("the defaults do not fit the schema: %v", err), "")
		return
	}
	// The settings become a configuration section at install (forge 05c)
	sec, err := config.ParseSchema(schema)
	if err != nil {
		c.add("settings.schema", fmt.Sprintf("the schema cannot be a settings section's: %v", err), "use the types object, string, integer, number, boolean, enum and array")
		return
	}
	if s.Defaults == nil {
		return
	}
	for _, e := range sec.Validate(s.Defaults) {
		c.add("settings.defaults."+e.Path, e.Message, "")
	}
}

func (c *check) triggers() {
	seen := map[string]bool{}
	for _, t := range c.m.Triggers {
		path := "triggers[" + t.ID + "]"
		switch {
		case !live.ValidToken(t.ID):
			c.add(path+".id", fmt.Sprintf("%q is not a trigger id", t.ID), "use letters, digits, _ and -")
		case seen[t.ID]:
			c.add(path+".id", fmt.Sprintf("the trigger %s is declared twice", t.ID), "")
		}
		seen[t.ID] = true
	}
}

func (c *check) requires() {
	for i, p := range c.m.Requires.Primitives {
		if !c.primitive(p) {
			c.add(fmt.Sprintf("requires.primitives[%d]", i), fmt.Sprintf("there is no primitive %q", p), "use a component id (mui.table) or a family (mui), as ui.listComponents lists them")
		}
	}
	seen := map[string]bool{}
	for i, a := range c.m.Requires.Apps {
		path := fmt.Sprintf("requires.apps[%d]", i)
		switch {
		case !live.ValidToken(a):
			c.add(path, fmt.Sprintf("%q is not an app id", a), "")
		case a == c.m.ID:
			c.add(path, "an app cannot require itself", "")
		case seen[a]:
			c.add(path, fmt.Sprintf("the app %s is required twice", a), "")
		}
		seen[a] = true
	}
}

// primitive tells whether p is a component, or the family of one.
func (c *check) primitive(p string) bool { return HasPrimitive(c.env.Catalog, p) }

// HasPrimitive tells whether p is a component of the catalog, or the
// family of one.
func HasPrimitive(catalog map[string]ui.Component, p string) bool {
	if _, ok := catalog[p]; ok {
		return true
	}
	for id := range catalog {
		if strings.HasPrefix(id, p+".") {
			return true
		}
	}
	return false
}
