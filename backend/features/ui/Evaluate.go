package ui

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// The template evaluator (forge 04b, documentation/ServerUI.md › Built in
// slice 26): it expands prefab instances into primitives on the server,
// inside a session's render, so the browser only ever receives primitives.
// Each instance is a region keyed by its node, so a change to its props,
// state or queries renders only that instance again; its state.* and
// query: bindings stay bindings of the session, so they still patch alone.

// definitionQuery names the query a session holds a prefab's definition
// under: its last published version, a pinned one, or its draft.
const definitionQuery = "__prefab:"

// instance is one mounted prefab instance of a session.
type instance struct {
	kp      string    // its key path, from the root's key, joined with '/'
	parent  *instance // the instance whose template holds it; nil for a page's root
	prefab  Prefab
	props   map[string]any
	on      map[string]On     // its events → its parent's handlers
	queries map[string]string // a declared query → the session's query name
	subs    map[string]string // a subscription's state path → the session's query name
	mounted bool              // its mount run is queued or done
	started bool              // its mount run is done
}

// scoped is where an instance's view state lives in the session's.
func (i *instance) scoped(path string) string {
	if path == "" {
		return "instances." + i.kp
	}
	return "instances." + i.kp + "." + path
}

// lifecycle is a prefab.logic run waiting until the tree is sent.
type lifecycle struct {
	inst  *instance
	event string
	gone  map[string]any // an unmounted instance's last state
}

const (
	onMount        = "ui.onMount"
	onPropsChanged = "ui.onPropsChanged"
	onQueryChanged = "ui.onQueryChanged"
	onUnmount      = "ui.onUnmount"
)

// LogicRunner runs prefab.logic graphs (uinodes.Runner): found is false
// when the graph is not one, or has no node for the event.
type LogicRunner interface {
	RunLogic(h *Handle, id, event string) (found bool, err error)
}

// scope is what a template node is evaluated in.
type scope struct {
	inst  *instance
	rows  map[string]any     // the rows of the repeats it is in
	slots map[string][]*Node // the content the instance's user passed
	outer *scope             // the scope slot content is evaluated in
}

// evaluator expands one session's prefab instances.
type evaluator struct {
	s       *Session
	draft   string         // the prefab whose draft a preview evaluates, not its published version
	samples bool           // the root takes its preview data as props
	design  bool           // the draft's nodes are tagged with their key paths (Node.Src)
	params  map[string]any // the page's params, which the root takes as props it declares
}

// PrefabPage is a page drawn from a published prefab (forge 04b question
// 1): its template, queries and handlers are the prefab's. props are its
// props; the page's params, an object, add those the prefab declares (a
// tab's params always hold its id, which a prefab need not take). variant,
// when set, is one of the prefab's variants (forge 04f), under props. Set
// the Descriptor, App and Title.
func PrefabPage(prefab, variant string, props map[string]any) Page {
	return Page{Description: "The prefab " + prefab, prefab: prefab, Render: func(r *Render) *Node {
		e := &evaluator{s: r.s}
		return e.root(r, prefab, variant, props)
	}}
}

// root evaluates the page's prefab as the instance at the root.
func (e *evaluator) root(r *Render, prefab, variant string, props map[string]any) *Node {
	all := maps.Clone(props)
	if all == nil {
		all = map[string]any{}
	}
	_ = json.Unmarshal(r.s.params, &e.params)
	n := &Node{Type: PrefabPrefix + prefab, Key: "root", Props: all, Variant: variant}
	return e.instance(r, &scope{}, n, nil, "")
}

var unsafeKey = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func kpOf(path []string) string { return strings.Join(path, "/") }

// nodes evaluates a template node in sc, under the node at path (keys from
// the root's); key, when set, replaces the node's own. A repeat gives a
// node per row and a when or an empty slot none.
func (e *evaluator) nodes(r *Render, sc *scope, n *Node, path []string, key string) []*Node {
	out := e.expand(r, sc, n, path, key)
	if n.Src != "" {
		for _, o := range out {
			if o.Src == "" {
				o.Src = n.Src
			}
		}
	}
	return out
}

func (e *evaluator) expand(r *Render, sc *scope, n *Node, path []string, key string) []*Node {
	if key == "" {
		key = n.Key
	}
	switch {
	case n.Type == SlotType:
		name, _ := n.Props["name"].(string)
		content := sc.slots[name]
		if len(content) == 0 {
			return nil
		}
		box := N("mui.box", key)
		inner := append(slices.Clone(path), key)
		for _, c := range content {
			box.Children = append(box.Children, e.nodes(r, sc.outer, c, inner, "")...)
		}
		return []*Node{box}
	case n.Type == RepeatType:
		return e.repeat(r, sc, n, path, key)
	case n.Type == WhenType:
		test, _ := n.Props["test"].(string)
		v, err := e.read(r, sc, test)
		if err != nil {
			return []*Node{ErrorNode(key, []Problem{{Message: err.Error()}})}
		}
		branch := n.Slots["else"]
		if truthy(v) {
			branch = n.Slots["then"]
		}
		if len(branch) == 0 {
			return nil
		}
		return e.nodes(r, sc, branch[0], path, "")
	case strings.HasPrefix(n.Type, PrefabPrefix):
		return []*Node{e.instance(r, sc, n, path, key)}
	}
	return []*Node{e.primitive(r, sc, n, path, key)}
}

func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	case int:
		return v != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}

// repeat gives its template once per row, keyed <repeat>_<row key>.
func (e *evaluator) repeat(r *Render, sc *scope, n *Node, path []string, key string) []*Node {
	each, _ := n.Props["each"].(string)
	as, _ := n.Props["as"].(string)
	rowKey, _ := n.Props["key"].(string)
	v, err := e.read(r, sc, each)
	if err != nil {
		return []*Node{ErrorNode(key, []Problem{{Message: err.Error()}})}
	}
	// A records query's page is its records
	if m, ok := v.(map[string]any); ok {
		v = m["records"]
	}
	list, ok := v.([]any)
	if v != nil && !ok {
		return []*Node{ErrorNode(key, []Problem{{Message: fmt.Sprintf("%s is not a list", each)}})}
	}
	if len(n.Children) == 0 {
		return nil
	}
	var out []*Node
	seen := map[string]bool{}
	for i, row := range list {
		rs := &scope{inst: sc.inst, rows: maps.Clone(sc.rows), slots: sc.slots, outer: sc.outer}
		if rs.rows == nil {
			rs.rows = map[string]any{}
		}
		rs.rows[as] = row
		k := strconv.Itoa(i)
		if rowKey != "" {
			if kv, err := e.read(r, rs, rowKey); err == nil && kv != nil {
				k = fmt.Sprint(kv)
			}
		}
		k = key + "_" + unsafeKey.ReplaceAllString(k, "_")
		if seen[k] {
			k += "_" + strconv.Itoa(i)
		}
		seen[k] = true
		out = append(out, e.nodes(r, rs, n.Children[0], path, k)...)
	}
	return out
}

// primitive copies a primitive with its bindings resolved in sc: state.*,
// query: and local.* stay bindings, scoped to the instance; the rest are
// read now.
func (e *evaluator) primitive(r *Render, sc *scope, n *Node, path []string, key string) *Node {
	out := &Node{Type: n.Type, Key: key, Props: maps.Clone(n.Props), Src: n.Src}
	here := append(slices.Clone(path), key)
	var problems []Problem
	bind := func(prop, src string) {
		if out.Bind == nil {
			out.Bind = map[string]string{}
		}
		out.Bind[prop] = src
	}
	for _, prop := range slices.Sorted(maps.Keys(n.Bind)) {
		from := n.Bind[prop]
		inst := sc.inst
		switch {
		case strings.HasPrefix(from, "local.") && inst != nil:
			bind(prop, "local."+inst.scoped(from[6:]))
		case strings.HasPrefix(from, "state.") && inst != nil:
			if err := e.declared(inst, from); err != nil {
				problems = append(problems, Problem{Message: err.Error()})
				continue
			}
			bind(prop, "state."+inst.scoped(from[6:]))
		case strings.HasPrefix(from, PresencePrefix) && inst != nil:
			held, err := e.presence(r, sc, from[len(PresencePrefix):])
			if err != nil {
				problems = append(problems, Problem{Message: fmt.Sprintf("%s: %v", prop, err)})
				continue
			}
			bind(prop, "query:"+held)
		case strings.HasPrefix(from, "query:") && inst != nil:
			name, rest, dotted := strings.Cut(from[6:], ".")
			held := inst.queries[name]
			if held == "" {
				problems = append(problems, Problem{Message: fmt.Sprintf("%s binds to the query %s, which %s does not hold", prop, name, inst.prefab.Name)})
				continue
			}
			if dotted {
				held += "." + rest
			}
			bind(prop, "query:"+held)
		default:
			v, err := e.read(r, sc, from)
			if err != nil {
				problems = append(problems, Problem{Message: fmt.Sprintf("%s: %v", prop, err)})
				continue
			}
			if out.Props == nil {
				out.Props = map[string]any{}
			}
			out.Props[prop] = v
		}
	}
	if len(n.On) > 0 {
		out.On = map[string]On{}
		for ev, o := range n.On {
			if sc.inst != nil {
				o.Handler = InstanceHandler(sc.inst.kp, o.Handler)
			}
			o.Payload = e.payload(r, sc, o.Payload)
			out.On[ev] = o
		}
	}
	for _, c := range n.Children {
		out.Children = append(out.Children, e.nodes(r, sc, c, here, "")...)
	}
	for _, name := range slices.Sorted(maps.Keys(n.Slots)) {
		if out.Slots == nil {
			out.Slots = map[string][]*Node{}
		}
		for _, c := range n.Slots[name] {
			out.Slots[name] = append(out.Slots[name], e.nodes(r, sc, c, here, "")...)
		}
	}
	if len(problems) > 0 {
		return ErrorNode(key, problems)
	}
	return out
}

// payload resolves an event's payload: "=" expressions now, local.* the
// browser's within the instance, the rest as they are.
func (e *evaluator) payload(r *Render, sc *scope, p map[string]any) map[string]any {
	if p == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range p {
		if s, ok := v.(string); ok {
			switch {
			case IsExpression(s):
				v, _ = e.read(r, sc, s)
			case strings.HasPrefix(s, "local.") && sc.inst != nil:
				v = "local." + sc.inst.scoped(s[6:])
			}
		}
		out[k] = v
	}
	return out
}

// InstanceHandler is the handler id an instance's template's events send:
// prefab:<key path>#<graph name>.
func InstanceHandler(kp, name string) string { return "prefab:" + kp + "#" + name }

func instanceHandler(handler string) (kp, name string, ok bool) {
	rest, ok := strings.CutPrefix(handler, "prefab:")
	if !ok {
		return "", "", false
	}
	i := strings.LastIndex(rest, "#")
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// declared reports a state.* binding to a value the prefab does not
// declare.
func (e *evaluator) declared(inst *instance, from string) error {
	name, _, _ := strings.Cut(strings.TrimPrefix(from, "state."), ".")
	if _, ok := inst.prefab.State[name]; !ok {
		return fmt.Errorf("%s binds to state.%s, which %s does not declare", from, name, inst.prefab.Name)
	}
	return nil
}

// read reads a binding in sc, recording what it read for tracking.
func (e *evaluator) read(r *Render, sc *scope, from string) (any, error) {
	if src, ok := strings.CutPrefix(from, "="); ok {
		return e.expression(r, sc, src)
	}
	inst := sc.inst
	if inst == nil {
		return nil, fmt.Errorf("%q is read outside a prefab", from)
	}
	if src, ok := strings.CutPrefix(from, PresencePrefix); ok {
		held, err := e.presence(r, sc, src)
		if err != nil {
			return nil, err
		}
		return r.Query(held), nil
	}
	if q, ok := strings.CutPrefix(from, "query:"); ok {
		name, rest, _ := strings.Cut(q, ".")
		held := inst.queries[name]
		if held == "" {
			return nil, fmt.Errorf("%q reads the query %s, which %s does not hold", from, name, inst.prefab.Name)
		}
		if rest == "" {
			return r.Query(held), nil
		}
		return r.Query(held, rest), nil
	}
	root, rest, _ := strings.Cut(from, ".")
	if row, ok := sc.rows[root]; ok {
		return getPath(row, rest), nil
	}
	switch root {
	case "props":
		name, _, _ := strings.Cut(rest, ".")
		if _, ok := inst.prefab.Props[name]; !ok {
			return nil, fmt.Errorf("%q reads props.%s, which %s does not declare", from, name, inst.prefab.Name)
		}
		return getPath(inst.props, rest), nil
	case "state":
		if err := e.declared(inst, from); err != nil {
			return nil, err
		}
		return r.State(inst.scoped(rest)), nil
	case "slots":
		return len(sc.slots[rest]) > 0, nil
	case "app":
		if path, ok := strings.CutPrefix(rest, "settings"); ok && (path == "" || path[0] == '.') {
			return e.appSettings(r, strings.TrimPrefix(path, ".")), nil
		}
		return getPath(e.app(), rest), nil
	case "user":
		return getPath(e.user(), rest), nil
	case "local":
		return nil, nil // the browser's
	}
	return nil, fmt.Errorf("%q binds to nothing a prefab can read", from)
}

func (e *evaluator) app() map[string]any {
	return map[string]any{"id": e.s.page.App, "page": e.s.page.ID}
}

// appSettingsQuery names the query a session holds its app's settings
// under, for app.settings bindings (forge 05c).
const appSettingsQuery = "__app.settings"

// appSettings reads the page's app's settings at path (all of them when
// empty), held as the app.settings query so a changed setting patches what
// reads it; nil on a page of no app, or while loading.
func (e *evaluator) appSettings(r *Render, path string) any {
	s := e.s
	if s.page.App == "" {
		return nil
	}
	if _, ok := s.queries[appSettingsQuery]; !ok {
		if s.o.Subs == nil || s.hold(appSettingsQuery, QueryRef{Query: "app.settings", Params: map[string]any{"app": s.page.App}}) != nil {
			return nil
		}
		s.fresh = append(s.fresh, s.queries[appSettingsQuery])
	}
	if path == "" {
		return r.Query(appSettingsQuery)
	}
	return r.Query(appSettingsQuery, path)
}

func (e *evaluator) user() map[string]any {
	p := e.s.principal
	return map[string]any{"id": strconv.FormatUint(p.UserID, 10), "tenant": strconv.FormatUint(p.TenantID, 10), "kind": p.Kind}
}

// expression runs an "=" binding over props, state, queries, app, user,
// slots and the rows, tracked by the names it reads (question 5).
func (e *evaluator) expression(r *Render, sc *scope, src string) (any, error) {
	inst := sc.inst
	if inst == nil {
		return nil, fmt.Errorf("an expression is read outside a prefab")
	}
	reads, err := expressionReads(src)
	if err != nil {
		return nil, err
	}
	state, _ := getPath(e.s.state, inst.scoped("")).(map[string]any)
	queries := map[string]any{}
	slotsOf := map[string]any{}
	for name := range inst.prefab.Slots {
		slotsOf[name] = len(sc.slots[name]) > 0
	}
	for _, rd := range reads {
		switch rd[0] {
		case "state":
			r.State(inst.scoped(rd[1]))
		case "queries":
			if held := inst.queries[rd[1]]; held != "" {
				queries[rd[1]] = r.Query(held)
			}
		}
	}
	app := e.app()
	if slices.ContainsFunc(reads, func(rd [2]string) bool { return rd[0] == "app" && rd[1] == "settings" }) {
		app["settings"] = e.appSettings(r, "")
	}
	env := map[string]any{"props": inst.props, "state": state, "queries": queries, "app": app, "user": e.user(), "slots": slotsOf}
	if env["state"] == nil {
		env["state"] = map[string]any{}
	}
	maps.Copy(env, sc.rows)
	return evalExpression(src, env)
}

// PresencePrefix starts a presence binding: presence:<binding> reads who
// is in the object the binding names ("<type>.<id>", such as
// record_task.123), as the live.presence query gives it: one entry per
// connection, {connection, userId, name, color, state, at}.
const PresencePrefix = "presence:"

// presence holds, for the instance, the live.presence query of the object
// a presence binding names, and gives the session's name for it. Being
// in an object is the browser's: presence.avatars joins one.
func (e *evaluator) presence(r *Render, sc *scope, src string) (string, error) {
	inst := sc.inst
	if inst == nil {
		return "", fmt.Errorf("presence is read outside a prefab")
	}
	v, err := e.read(r, sc, src)
	if err != nil {
		return "", err
	}
	object, _ := v.(string)
	typ, id, ok := strings.Cut(object, ".")
	if !ok || !live.ValidToken(typ) || !live.ValidToken(id) {
		return "", fmt.Errorf("%s gives %q, which is not an object (<type>.<id>)", src, object)
	}
	// A query's name holds no '.', which would start a path into its data
	held := inst.kp + "#" + PresencePrefix + typ + "~" + id
	if e.s.queries[held] == nil {
		if p := e.s.holdAs(inst, held, QueryRef{Query: "live.presence", Params: map[string]any{"object": object}}, ""); p != nil {
			return "", fmt.Errorf("%s", p.Message)
		}
	}
	return held, nil
}

// definition is a prefab's definition as the session holds it, or why it
// has none; nil with no problem is still loading.
func (e *evaluator) definition(r *Render, id string, version int, draft bool) (*Prefab, []Problem) {
	s := e.s
	if pin, ok := s.page.Pins[id]; ok && !draft && version == 0 {
		version = pin // an installed app's version pins it (forge 05d)
	}
	if s.page.Drafts && version == 0 {
		draft = true // an app's draft run (forge 05f)
	}
	name, params := definitionQuery+id, map[string]any{"id": id, "published": true}
	switch {
	case draft:
		name, params = definitionQuery+id+"~draft", map[string]any{"id": id}
	case version > 0:
		name, params = fmt.Sprintf("%s%s@%d", definitionQuery, id, version), map[string]any{"id": id, "version": version}
	}
	if !live.ValidToken(id) {
		return nil, []Problem{{Message: fmt.Sprintf("%q is not a prefab", id)}}
	}
	if _, ok := s.queries[name]; !ok && s.o.Subs != nil {
		if fail := s.hold(name, QueryRef{Query: "prefab.describe", Params: params}); fail != nil {
			return nil, []Problem{{Message: fail.Message}}
		}
		s.fresh = append(s.fresh, s.queries[name])
	}
	data := r.Query(name)
	q := s.queries[name]
	if q == nil {
		return nil, []Problem{{Message: "prefabs cannot be read here"}}
	}
	if q.failed != nil {
		what := "published prefab"
		switch {
		case draft:
			what = "prefab"
		case version > 0:
			what = fmt.Sprintf("version %d of a prefab", version)
		}
		return nil, []Problem{{Message: fmt.Sprintf("there is no %s %q", what, id), Fix: "publish it with prefab.publish, or use one prefab.list lists"}}
	}
	if data == nil {
		return nil, nil
	}
	if q.def == nil {
		raw, _ := json.Marshal(data)
		var v struct{ Prefab Prefab }
		if json.Unmarshal(raw, &v) != nil {
			return nil, []Problem{{Message: fmt.Sprintf("prefab %s cannot be read", id)}}
		}
		q.def = &v.Prefab
		if draft && e.design {
			tag(q.def.Template, "")
		}
	}
	return q.def, nil
}

// instance evaluates a prefab instance as a region keyed by its node: its
// props are read in sc, the user's scope, and its template in its own.
func (e *evaluator) instance(r *Render, sc *scope, n *Node, path []string, key string) *Node {
	if key == "" {
		key = n.Key
	}
	here := append(slices.Clone(path), key)
	kp := kpOf(here)
	if sc.inst != nil && kp == sc.inst.kp {
		// A prefab whose template's root is another prefab: both have the
		// root's node, so the inner one is named by its key below
		kp += "/" + n.Key
	}
	id, version, _ := InstanceOf(n.Type)
	props := maps.Clone(n.Props)
	if props == nil {
		props = map[string]any{}
	}
	var problems []Problem
	for _, prop := range slices.Sorted(maps.Keys(n.Bind)) {
		v, err := e.read(r, sc, n.Bind[prop])
		if err != nil {
			problems = append(problems, Problem{Message: fmt.Sprintf("%s: %v", prop, err)})
		}
		props[prop] = v
	}
	on := map[string]On{}
	for ev, o := range n.On {
		o.Payload = e.payload(r, sc, o.Payload)
		on[ev] = o
	}
	draft := e.draft != "" && sc.inst == nil && id == e.draft
	return r.Region(key, func(r *Render) *Node {
		out := ErrorNode(key, problems)
		if len(problems) == 0 {
			out = e.mount(r, sc, kp, id, version, draft, n.Variant, props, on, n.Slots, path, key)
		}
		if n.Src != "" {
			out.Src = n.Src
		}
		return out
	})
}

// mount renders an instance's template with its state, queries and
// lifecycle: a new one is mounted, one whose props changed is told so.
// Its props are its defaults, under its variant's, under those given.
func (e *evaluator) mount(r *Render, outer *scope, kp, id string, version int, draft bool, variant string, props map[string]any, on map[string]On, slots map[string][]*Node, path []string, key string) *Node {
	s := e.s
	s.seen[kp] = true
	def, problems := e.definition(r, id, version, draft)
	if def == nil {
		if problems == nil {
			return N("mui.box", key) // loading
		}
		return ErrorNode(key, problems)
	}
	if e.samples && outer.inst == nil && len(props) == 0 {
		props = maps.Clone(def.Preview.Props)
	}
	// The page's params, for the root's props it declares and was not given
	if outer.inst == nil && len(e.params) > 0 {
		props = maps.Clone(props)
		for k, v := range e.params {
			if _, declared := def.Props[k]; declared {
				if _, given := props[k]; !given {
					props[k] = v
				}
			}
		}
	}
	preset, ok := def.Variants[variant]
	if variant != "" && !ok {
		return ErrorNode(key, []Problem{{Message: fmt.Sprintf("%s has no variant %q", def.Name, variant), Fix: "use one of its variants (prefab.describe " + id + "), or none"}})
	}
	// Its props: what was given, then the variant's, then the defaults;
	// required ones present
	full := map[string]any{}
	for _, k := range slices.Sorted(maps.Keys(def.Props)) {
		d := def.Props[k]
		v, given := props[k]
		if !given || v == nil {
			v = preset.Props[k]
		}
		if v == nil {
			v = d.Default
		}
		switch {
		case v == nil && d.Required:
			problems = append(problems, Problem{Message: fmt.Sprintf("%s needs the prop %s", def.Name, k), Fix: "set it, or bind it"})
		case !fits(d.Type, d.Options, v):
			problems = append(problems, Problem{Message: fmt.Sprintf("the prop %s of %s is a %s", k, def.Name, d.Type)})
		}
		if v != nil {
			full[k] = v
		}
	}
	for _, k := range slices.Sorted(maps.Keys(props)) {
		if _, ok := def.Props[k]; !ok {
			problems = append(problems, Problem{Message: fmt.Sprintf("%s has no prop %s", def.Name, k)})
		}
	}
	if len(problems) > 0 {
		return ErrorNode(key, problems)
	}
	inst := s.instances[kp]
	if inst != nil && inst.prefab.ID != id {
		s.unmount(inst)
		inst = nil
	}
	if inst == nil {
		inst = &instance{kp: kp, queries: map[string]string{}, subs: map[string]string{}}
		s.instances[kp] = inst
	}
	inst.prefab, inst.parent, inst.on = *def, outer.inst, on
	if inst.started && !equalJSON(normalize(inst.props), normalize(full)) {
		s.runs = append(s.runs, lifecycle{inst: inst, event: onPropsChanged})
	}
	inst.props = full
	// Its state starts from the declared defaults
	for _, k := range slices.Sorted(maps.Keys(def.State)) {
		if d := def.State[k]; d.Default != nil && getPath(s.state, inst.scoped(k)) == nil {
			setPath(s.state, inst.scoped(k), d.Default)
		}
	}
	// Its queries, held while it is mounted (question 4)
	for _, name := range slices.Sorted(maps.Keys(def.Queries)) {
		if p := s.holdFor(inst, name, def.Queries[name]); p != nil {
			problems = append(problems, *p)
		}
	}
	if len(problems) > 0 {
		return ErrorNode(key, problems)
	}
	if !inst.mounted {
		inst.mounted = true
		s.runs = append(s.runs, lifecycle{inst: inst, event: onMount})
	}
	if def.Template == nil {
		return N("mui.box", key)
	}
	sc := &scope{inst: inst, slots: slots, outer: outer}
	out := e.nodes(r, sc, def.Template, path, key)
	switch len(out) {
	case 0:
		return N("mui.box", key)
	case 1:
		return out[0]
	}
	return N("mui.box", key, out...)
}

// holdFor holds an instance's declared query: props bound now, state
// bound as the session binds it, subscribed again when either changes.
func (s *Session) holdFor(inst *instance, name string, d QueryDecl) *Problem {
	params := maps.Clone(d.Params)
	if params == nil {
		params = map[string]any{}
	}
	bind := map[string]string{}
	for param, from := range d.Bind {
		root, rest, _ := strings.Cut(from, ".")
		switch root {
		case "props":
			if v := getPath(inst.props, rest); v != nil {
				params[param] = v
			}
		case "state":
			bind[param] = "state." + inst.scoped(rest)
		default:
			// An expression over the props, read now
			if src, ok := strings.CutPrefix(from, "="); ok {
				if v, err := evalExpression(src, map[string]any{"props": inst.props}); err == nil && v != nil {
					params[param] = v
				}
			}
		}
	}
	ref := QueryRef{Query: s.page.Query(d.Query), Params: params, Bind: bind}
	held := inst.kp + "#" + name
	inst.queries[name] = held
	q := s.queries[held]
	if q != nil && q.ref.Query == ref.Query && string(s.boundParams(ref)) == string(q.params) {
		return nil
	}
	return s.holdAs(inst, held, ref, "")
}

// holdAs holds a query for an instance, within the session's cap.
func (s *Session) holdAs(inst *instance, held string, ref QueryRef, into string) *Problem {
	if s.queries[held] == nil && len(s.queries) >= MaxSubscriptions {
		return &Problem{Message: fmt.Sprintf("this page holds %d queries, as many as it may", MaxSubscriptions),
			Fix: "pass records down as props from one query, rather than a query in each repeated instance"}
	}
	if s.o.Subs == nil {
		return &Problem{Message: "queries cannot be held here"}
	}
	if fail := s.hold(held, ref); fail != nil {
		return &Problem{Message: fail.Message}
	}
	q := s.queries[held]
	q.owner, q.into = inst, into
	s.fresh = append(s.fresh, q)
	return nil
}

// sweep unmounts the instances under the key path prefix that the last
// render did not see.
func (s *Session) sweep(prefix string) {
	for _, kp := range slices.Sorted(maps.Keys(s.instances)) {
		if s.seen[kp] || (prefix != "" && kp != prefix && !strings.HasPrefix(kp, prefix+"/")) {
			continue
		}
		s.unmount(s.instances[kp])
	}
}

// unmount ends an instance: its queries and subscriptions end, its state
// goes, and its unmount run is queued with the state it had.
func (s *Session) unmount(inst *instance) {
	delete(s.instances, inst.kp)
	for name, q := range s.queries {
		if q.owner == inst {
			q.cancel()
			delete(s.queries, name)
		}
	}
	gone, _ := getPath(s.state, inst.scoped("")).(map[string]any)
	setPath(s.state, inst.scoped(""), nil)
	if inst.started {
		s.runs = append(s.runs, lifecycle{inst: inst, event: onUnmount, gone: maps.Clone(gone)})
	}
}

// settle runs the queued lifecycle runs, in order, and sends what they
// change; runs they cause run in turn, a few rounds at most.
func (s *Session) settle() {
	for round := 0; round < 8 && len(s.runs) > 0; round++ {
		runs := s.runs
		s.runs = nil
		var actions []Action
		for _, run := range runs {
			actions = append(actions, s.runLogic(run)...)
		}
		s.send(actions)
	}
}

// runLogic runs an instance's prefab.logic graphs for one lifecycle event.
func (s *Session) runLogic(run lifecycle) []Action {
	inst := run.inst
	if run.event == onMount {
		inst.started = true
	}
	runner, ok := s.o.Graphs.(LogicRunner)
	if !ok || s.closed {
		return nil
	}
	if run.event != onUnmount && s.instances[inst.kp] != inst {
		return nil // gone before it ran
	}
	args := s.args
	args.Ctx = s.ctx
	h := &Handle{ServiceArgs: args, Params: s.params, s: s, request: newID(), inst: inst, gone: run.gone}
	for _, name := range slices.Sorted(maps.Keys(inst.prefab.Graphs)) {
		found, err := runner.RunLogic(h, inst.prefab.Graphs[name], run.event)
		if err != nil {
			f := args.Fail(500, Log.Wrapf(err, "%s: %s's logic failed", s.page.ID, inst.prefab.Name))
			s.o.Send(ErrorFrame{Type: "ui.error", Session: s.id, Handler: name, Code: f.Code, Message: f.Message, ErrorCode: f.ErrorCode})
		}
		_ = found
	}
	if run.event == onUnmount {
		return nil // an unmounted instance shows nothing (question 7)
	}
	return h.actions
}

// instanceEvent runs an instance's handler graph for an event of its
// template.
func (s *Session) instanceEvent(args features.ServiceArgs, kp, name string, payload map[string]any) {
	inst := s.instances[kp]
	if inst == nil {
		s.sendTree(true)
		return
	}
	h := &Handle{ServiceArgs: args, Payload: payload, Params: s.params, s: s, request: newID(), inst: inst}
	if err := s.runInstanceHandler(h, name); err != nil {
		s.handlerFailed(args, name, err)
	}
	s.flush(h.actions)
}

// runInstanceHandler runs the graph an instance's prefab names name.
func (s *Session) runInstanceHandler(h *Handle, name string) error {
	graph, ok := h.inst.prefab.Graphs[name]
	if !ok || s.o.Graphs == nil {
		return errNoHandler
	}
	found, err := s.o.Graphs.RunHandler(h, graph)
	if !found && err == nil {
		return errNoHandler
	}
	return err
}
