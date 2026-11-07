package installs

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// Running apps in the workbench (forge 05d, documentation/Apps.md › Built
// in slice 34): an installed app is a ui.appSource. Its main tab,
// app:<appId>:main, is its frame around the page at the current route,
// with the app's own back and forward (forge iteration question 2); each
// editor kind, app:<appId>:<kind>, is its page alone, titled by the
// manifest's templates over the page's data. Pages render the prefab
// versions the installed version pins and run its copies of their graphs,
// and an app's own commands and queries keep their names inside it.

// Handlers of the main page, which the frame's and pages' events reach.
const (
	handlerNavigate = "app.navigate" // {route}
	handlerBack     = "app.back"
	handlerForward  = "app.forward"
)

// maxHistory bounds a tab's back and forward lists.
const maxHistory = 50

// The main page's view state.
const (
	stateRoute   = "route"
	stateBack    = "back"
	stateForward = "forward"
)

// frameProps are the props the runtime gives a frame, each only when the
// frame declares it: the page's title, the app's name, the nav items as
// mui.list items ({id: route, primary, icon}), the selected item's route,
// the presence object of the page's subject, and whether back and forward
// go anywhere.
var frameProps = []string{"title", "appName", "nav", "selected", "subject", "canBack", "canForward"}

func contributePages(reg *extensions.Registry, deps func() Deps) {
	ui.AppSources.Register(reg, ui.AppSource{
		Descriptor: extensions.Descriptor{ID: "apps", Source: "apps"},
		Apps: func(args features.ServiceArgs) ([]ui.App, []ui.EditorKind) {
			return appsOf(cacheOf(deps().Projections).installed(args.Principal.TenantID))
		},
		Page: func(args features.ServiceArgs, id string) (ui.Page, bool, error) {
			return pageOf(args, deps(), id)
		},
		Changed: func(ctx context.Context, tenant uint64) <-chan struct{} {
			out := make(chan struct{}, 1)
			entries, err := deps().Projections.Watch(ctx, tenant, Projector, TargetType+".*")
			if err != nil {
				close(out)
				return out
			}
			go func() {
				defer close(out)
				started := false
				for e := range entries {
					if e == nil {
						started = true
						continue
					}
					if started {
						select {
						case out <- struct{}{}:
						default:
						}
					}
				}
			}()
			return out
		},
	})
}

// appsOf are the installed apps and their editor kinds, by app id.
func appsOf(installed map[string]apps.Install) ([]ui.App, []ui.EditorKind) {
	var out []ui.App
	var kinds []ui.EditorKind
	for _, id := range slices.Sorted(maps.Keys(installed)) {
		m := installed[id].Version.Manifest
		out = append(out, ui.App{Descriptor: extensions.Descriptor{ID: id, Source: "apps"}, Title: m.Name, Description: m.Description, Icon: m.Icon,
			Reach: reachOf(installed[id].Version)})
		if m.HomeRoute() != "" {
			kinds = append(kinds, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(id, "main"), Source: "apps"}, App: id, Kind: "main",
				Title: m.Name, Icon: m.Icon, Page: id + "/main"})
		}
		for _, k := range m.EditorKinds {
			title := k.Kind
			if p, _, ok := m.FindPage(k.Page); ok && p.Title != "" {
				title = p.Title
			}
			kinds = append(kinds, ui.EditorKind{Descriptor: extensions.Descriptor{ID: ui.KindID(id, k.Kind), Source: "apps"}, App: id, Kind: k.Kind,
				Title: title, Icon: firstOf(k.Icon, m.Icon), Page: id + "/" + k.Kind})
		}
	}
	return out, kinds
}

// reachOf is who may use an app: whoever may read one of its own entity
// types' records (its roles, the tenant's Owner and Administrators: the
// fence of forge 05c), else whoever may read the app.
func reachOf(v apps.Version) []ui.Reach {
	var out []ui.Reach
	for _, e := range v.Owned {
		out = append(out, ui.Reach{Action: "record.read", Resource: apps.Path(v.App, e)})
	}
	if len(out) == 0 {
		out = append(out, ui.Reach{Action: "app.read", Resource: "app:" + v.App})
	}
	return out
}

// pageOf resolves "<appId>/main" or "<appId>/<kind>" from the app's
// install; mine is false for an app that was never installed here.
func pageOf(args features.ServiceArgs, d Deps, id string) (ui.Page, bool, error) {
	app, name, ok := strings.Cut(id, "/")
	if !ok {
		return ui.Page{}, false, nil
	}
	tenant := args.Principal.TenantID
	c := cacheOf(d.Projections)
	in, running := c.installed(tenant)[app]
	if !running {
		if _, known := Get(args.Context(), d.Projections, tenant, app); known {
			return ui.Page{}, true, fmt.Errorf("the app %s is not installed", app)
		}
		return ui.Page{}, false, nil
	}
	v := in.Version
	m := v.Manifest
	page := ui.Page{
		Descriptor: extensions.Descriptor{ID: id, Source: "apps"}, App: app, Title: m.Name, Description: m.Description,
		Reach: reachOf(v), Pins: v.Prefabs,
		Graph: func(id string) (any, bool) {
			g, ok := v.Graphs[id]
			return g, ok
		},
		AppCommands: map[string]string{}, AppQueries: map[string]string{},
		Stale: func() bool {
			cur, ok := c.installed(tenant)[app]
			return !ok || cur.Version.Version != v.Version
		},
	}
	for _, cmd := range m.Commands {
		page.AppCommands[cmd.Type] = apps.Scoped(app, cmd.Type)
	}
	for _, q := range m.Queries {
		page.AppQueries[q.Name] = apps.Scoped(app, q.Name)
	}
	if name == "main" {
		home := m.HomeRoute()
		if home == "" {
			return ui.Page{}, true, fmt.Errorf("the app %s has no page without params to open on", app)
		}
		return mainPage(args, d, page, v, home), true, nil
	}
	i := slices.IndexFunc(m.EditorKinds, func(k apps.EditorKind) bool { return k.Kind == name })
	if i < 0 {
		return ui.Page{}, true, fmt.Errorf("the app %s has no editor kind %s since version %d", app, name, v.Version)
	}
	return kindPage(page, v, m.EditorKinds[i]), true, nil
}

// routeParams are the route's params from a page's params, as text.
func routeParams(route string, raw json.RawMessage) map[string]any {
	var given map[string]any
	_ = json.Unmarshal(raw, &given)
	names, _ := apps.RouteParams(route)
	out := map[string]any{}
	for _, n := range names {
		if v, ok := given[n]; ok && v != nil {
			out[n] = fmt.Sprint(v)
		}
	}
	return out
}

func asStrings(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// entityOf is the entity type of a page's record: the editor kind's
// subject type when the app uses that entity type, else the route's
// :entity param (the Records app's pages).
func entityOf(m apps.Manifest, k *apps.EditorKind, params map[string]any) string {
	if k != nil && k.Subject != nil && slices.Contains(m.Entities, k.Subject.Type) {
		return k.Subject.Type
	}
	e, _ := params["entity"].(string)
	return e
}

// subjectOf is what a page at path shows: its record when it has one
// (presence record_<entity>.<id>, as the starter prefabs' presence bars
// have it), else the app's page.
func subjectOf(app, path, entity string, params map[string]any, label string) *ui.Subject {
	if id, _ := params["id"].(string); entity != "" && id != "" {
		return &ui.Subject{Type: "record", ID: entity + "." + id, Label: label, Object: "record_" + entity + "." + id}
	}
	return &ui.Subject{Type: "appPage", ID: app + path, Label: label, Object: "app_" + app + "." + pathKey(path)}
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// pathKey is a path as a token: /task/123 is task_123, / is home.
func pathKey(path string) string {
	if k := strings.Trim(unsafe.ReplaceAllString(path, "_"), "_"); k != "" {
		return k
	}
	return "home"
}

var placeholder = regexp.MustCompile(`\{([A-Za-z0-9_.]+)\}`)

// fill fills a template's {dotted.path}s from data; empty when it has
// placeholders and none of them has a value yet.
func fill(template string, data map[string]any) string {
	found, filled := false, false
	out := placeholder.ReplaceAllStringFunc(template, func(m string) string {
		found = true
		var v any = data
		for seg := range strings.SplitSeq(m[1:len(m)-1], ".") {
			o, _ := v.(map[string]any)
			v = o[seg]
		}
		if v == nil {
			return ""
		}
		filled = true
		return fmt.Sprint(v)
	})
	if found && !filled {
		return ""
	}
	return strings.TrimSpace(out)
}

// kindPage is an editor kind's tab: its page's prefab alone, with the
// route's params as props. The page holds the record its params name (as
// record, its _title from the entity type's title field) for the title
// and subject templates, which also read params and the prefab's own
// props and queries by name (record is always the page's).
func kindPage(page ui.Page, v apps.Version, k apps.EditorKind) ui.Page {
	m := v.Manifest
	pg, _, _ := m.FindPage(k.Page)
	out := ui.AppPage(func(r *ui.Render) *ui.Node {
		return &ui.Node{Type: ui.PrefabPrefix + pg.Prefab, Key: "root", Props: routeParams(k.Page, r.Params())}
	})
	out.Descriptor, out.App, out.Description = page.Descriptor, page.App, page.Description
	out.Reach, out.Pins, out.Graph, out.AppCommands, out.AppQueries, out.Stale = page.Reach, page.Pins, page.Graph, page.AppCommands, page.AppQueries, page.Stale
	out.Title = firstOf(pg.Title, k.Kind)
	out.Queries = func(raw json.RawMessage) map[string]ui.QueryRef {
		params := routeParams(k.Page, raw)
		entity, id := entityOf(m, &k, params), params["id"]
		if entity == "" || id == nil {
			return nil
		}
		return map[string]ui.QueryRef{
			"record":   {Query: "records.get", Params: map[string]any{"entity": entity, "id": id}},
			"__entity": {Query: "entity.describe", Params: map[string]any{"id": entity, "published": true}},
		}
	}
	out.Heading = func(r *ui.Render) (string, *ui.Subject) {
		params := routeParams(k.Page, r.Params())
		data := map[string]any{"params": params}
		if inst := r.Instance("root"); inst != nil {
			maps.Copy(data, inst)
		}
		// The page's record wins over a query of the prefab's named record
		if rec, ok := r.Query("record").(map[string]any); ok {
			rec = maps.Clone(rec)
			if field, _ := r.Query("__entity", "entity.titleField").(string); field != "" && rec[field] != nil {
				rec["_title"] = rec[field]
			}
			data["record"] = rec
		}
		title := firstOf(fill(k.Title, data), out.Title)
		label := title
		if k.Subject != nil && k.Subject.Label != "" {
			label = firstOf(fill(k.Subject.Label, data), title)
		}
		return title, subjectOf(page.App, apps.FillRoute(k.Page, asStrings(params)), entityOf(m, &k, params), params, label)
	}
	return out
}

// mainPage is the app's own tab: its frame around the page at the
// current route (the view state's route), which navigation changes with
// the tab's own history.
func mainPage(args features.ServiceArgs, d Deps, page ui.Page, v apps.Version, home string) ui.Page {
	m := v.Manifest
	frame, declared := frameOf(args, d, v)
	// The page at a route; an unknown one (a page an upgrade removed) is home
	at := func(route string) (apps.Page, string, map[string]any) {
		pg, params, ok := m.FindPage(route)
		if !ok {
			route = home
			pg, params, _ = m.FindPage(home)
		}
		props := map[string]any{}
		for k, v := range params {
			props[k] = v
		}
		return pg, route, props
	}
	titleAt := func(pg apps.Page, props map[string]any) string {
		if pg.Title != "" {
			return pg.Title
		}
		if k, ok := m.KindFor(pg.Route); ok {
			if t := fill(k.Title, map[string]any{"params": props}); t != "" {
				return t
			}
		}
		return m.Name
	}
	subjectAt := func(pg apps.Page, route string, props map[string]any) *ui.Subject {
		var k *apps.EditorKind
		if kind, ok := m.KindFor(pg.Route); ok {
			k = &kind
		}
		return subjectOf(page.App, route, entityOf(m, k, props), props, m.Name+" › "+titleAt(pg, props))
	}
	out := ui.AppPage(func(r *ui.Render) *ui.Node {
		route, _ := r.State(stateRoute).(string)
		pg, route, props := at(route)
		content := &ui.Node{Type: ui.PrefabPrefix + pg.Prefab, Key: "page_" + pathKey(route), Props: props,
			On: map[string]ui.On{"navigate": {Handler: handlerNavigate}}}
		if frame == "" {
			content.Key = "root"
			return content
		}
		back, _ := r.State(stateBack).([]any)
		forward, _ := r.State(stateForward).([]any)
		nav := []any{}
		selected := ""
		for _, n := range m.Nav {
			nav = append(nav, map[string]any{"id": n.Route, "primary": n.Title, "icon": n.Icon})
			if n.Route == route {
				selected = n.Route
			}
		}
		all := map[string]any{"title": titleAt(pg, props), "appName": m.Name, "nav": nav, "selected": selected,
			"subject": subjectAt(pg, route, props).Object, "canBack": len(back) > 0, "canForward": len(forward) > 0}
		given := map[string]any{}
		for _, p := range frameProps {
			if declared[p] {
				given[p] = all[p]
			}
		}
		return &ui.Node{Type: ui.PrefabPrefix + frame, Key: "root", Props: given,
			On:    map[string]ui.On{"navigate": {Handler: handlerNavigate}, "back": {Handler: handlerBack}, "forward": {Handler: handlerForward}},
			Slots: map[string][]*ui.Node{apps.FrameSlot: {content}}}
	})
	out.Descriptor, out.App, out.Description, out.Title = page.Descriptor, page.App, page.Description, page.Title
	out.Reach, out.Pins, out.Graph, out.AppCommands, out.AppQueries, out.Stale = page.Reach, page.Pins, page.Graph, page.AppCommands, page.AppQueries, page.Stale
	out.Framed = frame != ""
	out.OnOpen = func(h *ui.Handle) error {
		var p struct{ Route string }
		_ = json.Unmarshal(h.Params, &p)
		_, route, _ := at(p.Route)
		h.Set(stateRoute, route)
		return nil
	}
	navigate := func(h *ui.Handle, route string) error {
		if _, _, ok := m.FindPage(route); !ok {
			h.Toast("warning", fmt.Sprintf("%s has no page at %s", m.Name, route))
			return nil
		}
		cur, _ := h.State(stateRoute).(string)
		if route == cur {
			return nil
		}
		back, _ := h.State(stateBack).([]any)
		h.Set(stateBack, push(back, cur))
		h.Set(stateForward, []any{})
		h.Set(stateRoute, route)
		return nil
	}
	out.Navigate = navigate
	// step moves one route along the history: from one list to the other
	step := func(from, to string) func(h *ui.Handle) error {
		return func(h *ui.Handle) error {
			list, _ := h.State(from).([]any)
			if len(list) == 0 {
				return nil
			}
			cur, _ := h.State(stateRoute).(string)
			other, _ := h.State(to).([]any)
			h.Set(to, push(other, cur))
			h.Set(from, slices.Clone(list[:len(list)-1]))
			h.Set(stateRoute, list[len(list)-1])
			return nil
		}
	}
	out.Handlers = map[string]func(h *ui.Handle) error{
		handlerNavigate: func(h *ui.Handle) error { return navigate(h, h.String("route")) },
		handlerBack:     step(stateBack, stateForward),
		handlerForward:  step(stateForward, stateBack),
	}
	out.Heading = func(r *ui.Render) (string, *ui.Subject) {
		route, _ := r.State(stateRoute).(string)
		pg, route, props := at(route)
		return m.Name, subjectAt(pg, route, props)
	}
	return out
}

// push appends to a history list, dropping its oldest past maxHistory.
func push(list []any, route string) []any {
	out := append(slices.Clone(list), route)
	if len(out) > maxHistory {
		out = out[len(out)-maxHistory:]
	}
	return out
}

// frameOf is the app's frame and the props it declares: the manifest's,
// at its pinned version, else the default frame when the tenant has it
// published; empty for none (the page alone, under the tab's header).
func frameOf(args features.ServiceArgs, d Deps, v apps.Version) (string, map[string]bool) {
	id := strings.TrimPrefix(v.Manifest.Frame, ui.PrefabPrefix)
	if id == "" {
		id = apps.DefaultFrame
	}
	params := map[string]any{"id": id, "published": true}
	if pin, ok := v.Prefabs[id]; ok {
		params = map[string]any{"id": id, "version": pin}
	} else if _, ok, err := d.Projections.Get(args.Context(), args.Principal.TenantID, prefabs.PublishedProjector, prefabs.TargetType+"."+id); err != nil || !ok {
		return "", nil // the tenant has no such frame published
	}
	if d.Nodes.Subscriptions == nil {
		return "", nil
	}
	raw, _ := json.Marshal(params)
	data, _, fail := d.Nodes.Subscriptions.Once(args, "prefab.describe", raw)
	if fail != nil {
		return "", nil
	}
	b, _ := json.Marshal(data)
	var view struct{ Prefab ui.Prefab }
	if json.Unmarshal(b, &view) != nil {
		return "", nil
	}
	if _, ok := view.Prefab.Slots[apps.FrameSlot]; !ok {
		return "", nil
	}
	declared := map[string]bool{}
	for p := range view.Prefab.Props {
		declared[p] = true
	}
	return id, declared
}
