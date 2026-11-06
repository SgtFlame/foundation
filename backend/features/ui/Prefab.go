package ui

import (
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Prefab is a prefab's definition (forge 04a, documentation/ServerUI.md ›
// Built in slice 25): a component tree with the interface its users see
// (props, events, slots), its own state and queries, and the graphs behind
// it. It is the ui.prefab projection's draft and what a published version
// holds; ui/prefabs edits it with prefab.* commands.
type Prefab struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Owner is the app the prefab belongs to (app:<appId>); empty puts it
	// in the tenant's library. Any prefab of the tenant can use any other.
	Owner    string               `json:"owner,omitempty"`
	Props    map[string]PropDecl  `json:"props"`
	State    map[string]StateDecl `json:"state"`
	Events   map[string]EventDecl `json:"events"`
	Slots    map[string]SlotDecl  `json:"slots"`
	Queries  map[string]QueryDecl `json:"queries"`
	Template *Node                `json:"template,omitempty"`
	Graphs   map[string]string    `json:"graphs"` // a name the template's handlers use → a graph id
	Preview  PrefabPreview        `json:"preview"`
	Version  int                  `json:"version"` // the published version; 0 in a draft never published
}

// PropDecl is one prop of a prefab's interface.
type PropDecl struct {
	Type        string   `json:"type"`              // one of PrefabTypes
	Entity      string   `json:"entity,omitempty"`  // record, records: the entity type
	Options     []string `json:"options,omitempty"` // enum
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// StateDecl is one value of a prefab instance's own view state.
type StateDecl struct {
	Type        string   `json:"type"`
	Entity      string   `json:"entity,omitempty"`
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// EventDecl is an event a prefab raises, with its payload's fields and
// their types.
type EventDecl struct {
	Description string            `json:"description,omitempty"`
	Payload     map[string]string `json:"payload,omitempty"`
}

// SlotDecl is a place where content its user passes goes.
type SlotDecl struct {
	Description string `json:"description,omitempty"`
}

// QueryDecl is a query a prefab holds while mounted: a registered query
// or a data.query graph, with params, which may bind to props and state as
// QueryRef.Bind does.
type QueryDecl struct {
	Query       string            `json:"query"`
	Params      map[string]any    `json:"params,omitempty"`
	Bind        map[string]string `json:"bind,omitempty"`
	Description string            `json:"description,omitempty"`
}

// PrefabPreview is the sample data the Prefab Editor renders with. It is
// part of the draft, not of the interface.
type PrefabPreview struct {
	Props map[string]any `json:"props,omitempty"`
}

// Graph kinds a prefab's graphs may be (ui/nodes registers them).
const (
	GraphKindHandler = "ui.handler"
	GraphKindPrefab  = "prefab.logic"
)

// PrefabPrefix makes a component type of a prefab: prefab.<id>.
const PrefabPrefix = "prefab."

// SlotType is the node type that places a slot's content in a template.
const SlotType = "slot"

// PrefabType is one type a prefab's props, state and event payloads may
// have.
type PrefabType struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// PrefabTypes are the types of props, state and payload fields.
var PrefabTypes = []PrefabType{
	{"string", "Text"},
	{"number", "A number"},
	{"integer", "A whole number"},
	{"bool", "True or false"},
	{"date", "A date, YYYY-MM-DD"},
	{"dateTime", "A moment, RFC 3339"},
	{"enum", "One of options"},
	{"json", "Any JSON value"},
	{"record", "One record of entity (its fields by id, with _id)"},
	{"records", "A list of records of entity"},
}

var name = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// ValidName reports whether s can name a prop, state value, event, slot,
// query or graph: letters, digits and '_', starting with a letter, so a
// binding reads it as props.<name>.
func ValidName(s string) bool { return name.MatchString(s) }

func knownType(t string) bool {
	return slices.ContainsFunc(PrefabTypes, func(p PrefabType) bool { return p.ID == t })
}

func typeList() string {
	ids := make([]string, len(PrefabTypes))
	for i, t := range PrefabTypes {
		ids[i] = t.ID
	}
	return strings.Join(ids, ", ")
}

// fits reports whether a literal value fits a declared type.
func fits(typ string, options []string, v any) bool {
	if v == nil {
		return true
	}
	switch typ {
	case "string", "date", "dateTime":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "bool":
		_, ok := v.(bool)
		return ok
	case "enum":
		s, ok := v.(string)
		return ok && slices.Contains(options, s)
	case "record":
		_, ok := v.(map[string]any)
		return ok
	case "records":
		_, ok := v.([]any)
		return ok
	}
	return true
}

// PrefabEnv is what a prefab is checked against beside itself. A nil
// function skips its checks.
type PrefabEnv struct {
	Catalog map[string]Component // the primitives
	// Prefab is another prefab of the tenant, as its instances see it: its
	// last published version, else its draft.
	Prefab func(id string) (Prefab, bool)
	// Uses are the prefabs another prefab's draft and published version
	// use, for finding a prefab that contains itself.
	Uses func(id string) []string
	// Version is a published version of another prefab, for an instance
	// that pins one (prefab.<id>@<n>); nil checks it against Prefab.
	Version func(id string, version int) (Prefab, bool)
	Entity  func(id string) bool                   // an entity type exists
	Query   func(name string) bool                 // a query or data.query graph exists
	Graph   func(id string) (kind string, ok bool) // a graph's kind
}

// PrefabsUsed are the prefab ids a template's nodes name.
func PrefabsUsed(root *Node) []string {
	var out []string
	walkAll(root, nil, func(_ []string, n *Node) {
		if id, _, ok := InstanceOf(n.Type); ok && !slices.Contains(out, id) {
			out = append(out, id)
		}
	})
	return out
}

// walkAll visits n, its children and its slots' content, depth first,
// with each one's key path.
func walkAll(n *Node, path []string, fn func(path []string, n *Node)) {
	if n == nil {
		return
	}
	path = append(slices.Clone(path), n.Key)
	fn(path, n)
	for _, c := range n.Children {
		walkAll(c, path, fn)
	}
	for _, s := range slices.Sorted(maps.Keys(n.Slots)) {
		for _, c := range n.Slots[s] {
			walkAll(c, path, fn)
		}
	}
}

// CheckPrefab is every problem with a prefab's definition, each at a path
// within it (template.root/row/title.props.variant) with a fix a model can
// act on. Template paths are node keys from the root, joined with '/'.
func CheckPrefab(p Prefab, env PrefabEnv) []live.Problem {
	c := &prefabCheck{p: p, env: env}
	c.info()
	c.iface()
	c.graphs()
	if p.Template != nil {
		c.node(p.Template, nil)
	}
	c.preview()
	return c.problems
}

type prefabCheck struct {
	p        Prefab
	env      PrefabEnv
	problems []live.Problem
	handlers map[string]bool // graph names the template's handlers use
	rows     []string        // the row names of the repeats above the node being checked
}

func (c *prefabCheck) add(path, message, fix string) {
	c.problems = append(c.problems, live.Problem{Path: path, Message: message, Fix: fix})
}

func (c *prefabCheck) info() {
	if strings.TrimSpace(c.p.Name) == "" {
		c.add("name", "a prefab needs a name", "set one with prefab.setInfo {name}")
	}
	if c.p.Owner != "" {
		if app, ok := strings.CutPrefix(c.p.Owner, "app:"); !ok || !live.ValidToken(app) {
			c.add("owner", fmt.Sprintf("%q is not an owner", c.p.Owner), "use app:<appId>, or leave it empty for the tenant's library")
		}
	}
}

// typed checks a declared type and its entity or options.
func (c *prefabCheck) typed(path, typ, entity string, options []string) {
	switch {
	case !knownType(typ):
		c.add(path+".type", fmt.Sprintf("%q is not a type", typ), "use one of "+typeList())
	case (typ == "record" || typ == "records") && entity == "":
		c.add(path+".entity", "a "+typ+" needs its entity type", "set entity to an entity type's id (entity.list)")
	case (typ == "record" || typ == "records") && c.env.Entity != nil && !c.env.Entity(entity):
		c.add(path+".entity", fmt.Sprintf("there is no entity type %q", entity), "use one entity.list lists")
	case typ == "enum" && len(options) == 0:
		c.add(path+".options", "an enum needs its options", "list the values it may take")
	}
}

func (c *prefabCheck) names(section string, keys iter.Seq[string]) {
	for k := range keys {
		if !ValidName(k) {
			c.add(section+"."+k, fmt.Sprintf("%q is not a valid name", k), "use letters, digits and '_', starting with a letter")
		}
	}
}

func (c *prefabCheck) iface() {
	p := c.p
	c.names("props", maps.Keys(p.Props))
	c.names("state", maps.Keys(p.State))
	c.names("events", maps.Keys(p.Events))
	c.names("slots", maps.Keys(p.Slots))
	c.names("queries", maps.Keys(p.Queries))
	for _, k := range slices.Sorted(maps.Keys(p.Props)) {
		d := p.Props[k]
		c.typed("props."+k, d.Type, d.Entity, d.Options)
		if !fits(d.Type, d.Options, d.Default) {
			c.add("props."+k+".default", fmt.Sprintf("the default does not fit %s", d.Type), "")
		}
	}
	for _, k := range slices.Sorted(maps.Keys(p.State)) {
		d := p.State[k]
		c.typed("state."+k, d.Type, d.Entity, d.Options)
		if !fits(d.Type, d.Options, d.Default) {
			c.add("state."+k+".default", fmt.Sprintf("the default does not fit %s", d.Type), "")
		}
	}
	for _, k := range slices.Sorted(maps.Keys(p.Events)) {
		for f, t := range p.Events[k].Payload {
			if !ValidName(f) {
				c.add("events."+k+".payload."+f, fmt.Sprintf("%q is not a valid field name", f), "use letters, digits and '_', starting with a letter")
			}
			if !knownType(t) {
				c.add("events."+k+".payload."+f, fmt.Sprintf("%q is not a type", t), "use one of "+typeList())
			}
		}
	}
	for _, k := range slices.Sorted(maps.Keys(p.Queries)) {
		q := p.Queries[k]
		path := "queries." + k
		switch {
		case q.Query == "":
			c.add(path+".query", "a query needs the query it reads", "name a registered query (live.listQueries) or a data.query graph")
		case c.env.Query != nil && !c.env.Query(q.Query):
			c.add(path+".query", fmt.Sprintf("there is no query %q", q.Query), "name a registered query (live.listQueries) or a data.query graph")
		}
		for param, from := range q.Bind {
			root, _, _ := strings.Cut(from, ".")
			ok := (root == "props" && c.declared(p.Props, from, "props.")) || (root == "state" && c.declared(p.State, from, "state."))
			if !ok {
				c.add(path+".bind."+param, fmt.Sprintf("param %s reads %q, which is not a declared prop or state value", param, from), "bind it to props.<prop> or state.<value>")
			}
		}
	}
}

// declared reports whether a path's first name after prefix is a key of m.
func (c *prefabCheck) declared(m any, path, prefix string) bool {
	rest, ok := strings.CutPrefix(path, prefix)
	if !ok {
		return false
	}
	n, _, _ := strings.Cut(rest, ".")
	switch m := m.(type) {
	case map[string]PropDecl:
		_, ok = m[n]
	case map[string]StateDecl:
		_, ok = m[n]
	case map[string]SlotDecl:
		_, ok = m[n]
	}
	return ok
}

func (c *prefabCheck) graphs() {
	c.handlers = map[string]bool{}
	walkAll(c.p.Template, nil, func(_ []string, n *Node) {
		for _, o := range n.On {
			c.handlers[o.Handler] = true
		}
	})
	for _, k := range slices.Sorted(maps.Keys(c.p.Graphs)) {
		id, path := c.p.Graphs[k], "graphs."+k
		if !ValidName(k) {
			c.add(path, fmt.Sprintf("%q is not a valid name", k), "use letters, digits and '_', starting with a letter")
		}
		if c.env.Graph == nil {
			continue
		}
		kind, ok := c.env.Graph(id)
		switch {
		case !ok:
			c.add(path, fmt.Sprintf("there is no graph %q", id), "make it with bp.create {kind: \"ui.handler\"} or {kind: \"prefab.logic\"}, then attach it")
		case c.handlers[k] && kind != GraphKindHandler:
			c.add(path, fmt.Sprintf("graph %s handles an event, but it is a %s graph", id, kind), "attach a ui.handler graph under this name")
		case kind != GraphKindHandler && kind != GraphKindPrefab:
			c.add(path, fmt.Sprintf("graph %s is a %s graph", id, kind), "attach ui.handler graphs for events and prefab.logic graphs for the lifecycle")
		}
	}
}

func (c *prefabCheck) preview() {
	for _, k := range slices.Sorted(maps.Keys(c.p.Preview.Props)) {
		d, ok := c.p.Props[k]
		switch {
		case !ok:
			c.add("preview.props."+k, fmt.Sprintf("there is no prop %q", k), "give sample values for the declared props only")
		case !fits(d.Type, d.Options, c.p.Preview.Props[k]):
			c.add("preview.props."+k, fmt.Sprintf("the sample value does not fit %s", d.Type), "")
		}
	}
}

// node checks one node and its subtree; parent is the key path above it.
func (c *prefabCheck) node(n *Node, parent []string) {
	keys := append(slices.Clone(parent), n.Key)
	path := "template." + strings.Join(keys, "/")
	if !live.ValidToken(n.Key) {
		c.add(path+".key", fmt.Sprintf("%q is not a valid key", n.Key), "use letters, digits, '-' and '_', or leave it out to get one from the type")
	}
	var (
		events   []string
		slots    []string
		children bool
		known    bool // the type is known, so its events and slots are
	)
	switch {
	case n.Type == SlotType:
		c.slotNode(n, path)
	case n.Type == RepeatType:
		c.repeatNode(n, path)
		as, _ := n.Props["as"].(string)
		c.rows = append(c.rows, as)
		defer func() { c.rows = c.rows[:len(c.rows)-1] }()
		children = true
	case n.Type == WhenType:
		c.whenNode(n, path)
		slots, known = []string{"then", "else"}, true
	case strings.HasPrefix(n.Type, PrefabPrefix):
		events, slots = c.instance(n, path)
		known = events != nil
	default:
		comp, ok := c.env.Catalog[n.Type]
		if c.env.Catalog != nil && !ok {
			c.add(path+".type", fmt.Sprintf("there is no component %q", n.Type), "use one ui.listComponents lists, slot, or prefab.<id> of another prefab")
			break
		}
		if ok {
			c.props(comp, n, path)
			for _, e := range comp.Events {
				events = append(events, e.Name)
			}
			slots, children, known = comp.Slots, comp.Children, true
		}
		if len(n.Children) > 0 && ok && !children {
			c.add(path+".children", fmt.Sprintf("%s takes no children", n.Type), "insert them into a container such as mui.stack")
		}
	}
	if known || n.Type == SlotType || n.Type == RepeatType {
		for _, ev := range slices.Sorted(maps.Keys(n.On)) {
			if !slices.Contains(events, ev) {
				c.add(path+".on."+ev, fmt.Sprintf("%s emits no event %q", n.Type, ev), "use one of: "+strings.Join(events, ", "))
			}
		}
	}
	for _, ev := range slices.Sorted(maps.Keys(n.On)) {
		h := n.On[ev].Handler
		if _, ok := c.p.Graphs[h]; !ok {
			c.add(path+".on."+ev+".handler", fmt.Sprintf("the handler %q is not a graph of the prefab", h), "make a ui.handler graph with bp.create, then prefab.attachGraph {name: \""+h+"\", graph}")
		}
	}
	for _, prop := range slices.Sorted(maps.Keys(n.Bind)) {
		c.bind(path+".bind."+prop, n.Bind[prop])
	}
	if known {
		for s := range n.Slots {
			if !slices.Contains(slots, s) {
				c.add(path+".slots."+s, fmt.Sprintf("%s has no slot %q", n.Type, s), "")
			}
		}
	}
	seen := map[string]bool{}
	visit := func(ch *Node) {
		if ch.Key != "" && seen[ch.Key] {
			c.add(path+"/"+ch.Key+".key", fmt.Sprintf("the key %q is used twice under %s", ch.Key, n.Key), "give it a key unique among its siblings")
			return
		}
		seen[ch.Key] = true
		c.node(ch, keys)
	}
	for _, ch := range n.Children {
		visit(ch)
	}
	for _, s := range slices.Sorted(maps.Keys(n.Slots)) {
		for _, ch := range n.Slots[s] {
			visit(ch)
		}
	}
}

// props checks a primitive's props against its schema; a bound prop
// counts as present, as in Validate.
func (c *prefabCheck) props(comp Component, n *Node, path string) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	_ = json.Unmarshal(comp.Props, &schema)
	names := slices.Sorted(maps.Keys(schema.Properties))
	for _, k := range slices.Sorted(maps.Keys(n.Props)) {
		s, ok := schema.Properties[k]
		if !ok {
			c.add(path+".props."+k, fmt.Sprintf("%s has no prop %q", n.Type, k), "use one of: "+strings.Join(names, ", "))
			continue
		}
		one, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]json.RawMessage{k: s}})
		v, _ := json.Marshal(map[string]any{k: n.Props[k]})
		if err := tools.CheckArgs(one, v); err != nil {
			c.add(path+".props."+k, fmt.Sprintf("%s props: %v", n.Type, err), "ui.describeComponent "+n.Type+" gives its props schema")
		}
	}
	if sx, ok := n.Props["sx"]; ok {
		for _, m := range CheckSx(sx) {
			c.add(path+".props.sx", m, SxTokens)
		}
	}
	for k := range n.Bind {
		if _, ok := schema.Properties[k]; !ok && len(schema.Properties) > 0 {
			c.add(path+".bind."+k, fmt.Sprintf("%s has no prop %q", n.Type, k), "use one of: "+strings.Join(names, ", "))
		}
	}
	for _, k := range schema.Required {
		_, set := n.Props[k]
		_, bound := n.Bind[k]
		if !set && !bound {
			c.add(path+".props."+k, fmt.Sprintf("%s needs the prop %s", n.Type, k), "set it, or bind it")
		}
	}
}

// instance checks a node that is another prefab; it gives that prefab's
// events and slots.
func (c *prefabCheck) instance(n *Node, path string) (events, slots []string) {
	id, version, _ := InstanceOf(n.Type)
	if id == c.p.ID {
		c.add(path+".type", fmt.Sprintf("prefab %s cannot contain itself", id), "")
		return nil, nil
	}
	if version < 0 {
		c.add(path+".type", fmt.Sprintf("%q does not name a version", n.Type), "use prefab.<id> for the last published version, or prefab.<id>@<n> to pin version n")
		return nil, nil
	}
	if c.env.Prefab == nil {
		return nil, nil
	}
	other, ok := c.env.Prefab(id)
	if ok && version > 0 && c.env.Version != nil {
		if other, ok = c.env.Version(id, version); !ok {
			c.add(path+".type", fmt.Sprintf("prefab %s has no published version %d", id, version), "pin a version prefab.describe {id, version} finds, or follow the last with prefab."+id)
			return nil, nil
		}
	}
	if !ok {
		c.add(path+".type", fmt.Sprintf("there is no prefab %q", id), "use one prefab.list lists, or make it with prefab.create")
		return nil, nil
	}
	if c.contains(id) {
		c.add(path+".type", fmt.Sprintf("prefab %s contains %s, so it cannot be used inside it", id, c.p.ID), "")
	}
	for _, k := range slices.Sorted(maps.Keys(n.Props)) {
		d, ok := other.Props[k]
		switch {
		case !ok:
			c.add(path+".props."+k, fmt.Sprintf("prefab %s has no prop %q", id, k), "use one of its props (prefab.describe "+id+")")
		case !fits(d.Type, d.Options, n.Props[k]):
			c.add(path+".props."+k, fmt.Sprintf("prop %s of %s is a %s", k, id, d.Type), "")
		}
	}
	for k := range n.Bind {
		if _, ok := other.Props[k]; !ok {
			c.add(path+".bind."+k, fmt.Sprintf("prefab %s has no prop %q", id, k), "use one of its props (prefab.describe "+id+")")
		}
	}
	for _, k := range slices.Sorted(maps.Keys(other.Props)) {
		_, set := n.Props[k]
		_, bound := n.Bind[k]
		if other.Props[k].Required && !set && !bound {
			c.add(path+".props."+k, fmt.Sprintf("prefab %s needs the prop %s", id, k), "set it, or bind it")
		}
	}
	if len(n.Children) > 0 {
		c.add(path+".children", "a prefab takes no children", "pass content in its slots")
	}
	events, slots = []string{}, []string{}
	for e := range other.Events {
		events = append(events, e)
	}
	for s := range other.Slots {
		slots = append(slots, s)
	}
	slices.Sort(events)
	return events, slots
}

// contains reports whether prefab id uses this prefab, directly or
// through others.
func (c *prefabCheck) contains(id string) bool {
	if c.env.Uses == nil {
		return false
	}
	seen := map[string]bool{}
	var walk func(id string) bool
	walk = func(id string) bool {
		if id == c.p.ID {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		return slices.ContainsFunc(c.env.Uses(id), walk)
	}
	return walk(id)
}

func (c *prefabCheck) slotNode(n *Node, path string) {
	s, _ := n.Props["name"].(string)
	if _, ok := c.p.Slots[s]; !ok {
		c.add(path+".props.name", fmt.Sprintf("there is no slot %q", s), "declare it with prefab.setInterface {slots}, and name it in props.name")
	}
	for k := range n.Props {
		if k != "name" {
			c.add(path+".props."+k, fmt.Sprintf("a slot has no prop %q", k), "a slot takes only name")
		}
	}
	if len(n.Children) > 0 || len(n.Slots) > 0 {
		c.add(path+".children", "a slot takes no children", "its content comes from the prefab's user")
	}
}

// repeatNode checks a repeat: what it repeats over, its row name, the
// row key and its child, the row's template (none yet repeats nothing).
func (c *prefabCheck) repeatNode(n *Node, path string) {
	each, _ := n.Props["each"].(string)
	as, _ := n.Props["as"].(string)
	key, _ := n.Props["key"].(string)
	if each == "" {
		c.add(path+".props.each", "a repeat needs what it repeats over", "set each to a binding of a list, such as query:<name> or props.<prop>")
	} else {
		c.bind(path+".props.each", each)
	}
	switch {
	case !ValidName(as):
		c.add(path+".props.as", fmt.Sprintf("%q is not a valid row name", as), "name the row, such as task, with letters, digits and '_'")
	case slices.Contains(expressionRoots, as) || as == "local" || slices.Contains(c.rows, as):
		c.add(path+".props.as", fmt.Sprintf("the row name %q is already taken", as), "choose another name")
	}
	if key != "" && !strings.HasPrefix(key, as+".") && key != as {
		c.add(path+".props.key", fmt.Sprintf("the row key %q does not read the row %s", key, as), "use "+as+".<field>, such as "+as+"._id")
	}
	for k := range n.Props {
		if k != "each" && k != "as" && k != "key" {
			c.add(path+".props."+k, fmt.Sprintf("a repeat has no prop %q", k), "a repeat takes each, as and key")
		}
	}
	if len(n.Children) > 1 {
		c.add(path+".children", "a repeat takes one child, the row's template", "wrap them in a container such as mui.stack")
	}
	if len(n.Slots) > 0 {
		c.add(path+".slots", "a repeat has no slots", "")
	}
}

// whenNode checks a when: its test, and at most one node in each of its
// then and else slots.
func (c *prefabCheck) whenNode(n *Node, path string) {
	test, _ := n.Props["test"].(string)
	if test == "" {
		c.add(path+".props.test", "a when needs its test", "set test to a binding such as state.editing, or an expression starting with =")
	} else {
		c.bind(path+".props.test", test)
	}
	for k := range n.Props {
		if k != "test" {
			c.add(path+".props."+k, fmt.Sprintf("a when has no prop %q", k), "a when takes test")
		}
	}
	for _, s := range []string{"then", "else"} {
		if len(n.Slots[s]) > 1 {
			c.add(path+".slots."+s, fmt.Sprintf("a when's %s takes one node", s), "wrap them in a container such as mui.stack")
		}
	}
	if len(n.Children) > 0 {
		c.add(path+".children", "a when takes no children", "put the nodes in its then and else slots")
	}
}

// bind checks that a binding reads only what the prefab declares.
func (c *prefabCheck) bind(path, from string) {
	if src, ok := strings.CutPrefix(from, "="); ok {
		if err := checkExpression(src, c.rows); err != nil {
			c.add(path, fmt.Sprintf("the expression %q: %v", src, err), "an expression reads props, state, queries, app, user, slots and the rows of the repeats it is in")
			return
		}
		reads, _ := expressionReads(src)
		for _, r := range reads {
			if r[1] == "" {
				continue
			}
			switch r[0] {
			case "props", "state", "slots":
				c.bind(path, r[0]+"."+r[1])
			case "queries":
				c.bind(path, "query:"+r[1])
			}
		}
		return
	}
	root, _, _ := strings.Cut(from, ".")
	if slices.Contains(c.rows, root) {
		return
	}
	if q, ok := strings.CutPrefix(from, "query:"); ok {
		name, _, _ := strings.Cut(q, ".")
		if _, ok := c.p.Queries[name]; !ok {
			c.add(path, fmt.Sprintf("%q reads the query %q, which the prefab does not declare", from, name), "declare it with prefab.setInterface {queries}")
		}
		return
	}
	var ok bool
	switch root {
	case "local", "app", "user":
		return
	case "props":
		ok = c.declared(c.p.Props, from, "props.")
	case "state":
		ok = c.declared(c.p.State, from, "state.")
	case "slots":
		ok = c.declared(c.p.Slots, from, "slots.")
	default:
		c.add(path, fmt.Sprintf("%q is not a binding a prefab can read", from), "bind to props.<prop>, state.<value>, slots.<slot>, query:<name>, local.<path>, app.<setting>, user.<field>, a repeat's row, or an expression starting with =")
		return
	}
	if !ok {
		n, _, _ := strings.Cut(strings.TrimPrefix(from, root+"."), ".")
		c.add(path, fmt.Sprintf("%q reads %s.%s, which the prefab does not declare", from, root, n), "declare it with prefab.setInterface, or bind to a declared one")
	}
}
