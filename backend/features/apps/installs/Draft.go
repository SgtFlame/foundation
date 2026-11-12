package installs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Running an app's draft (forge 05f question 3, documentation/Apps.md ›
// Built in slice 36): the app's authors (app.write on the app) open
// <appId>/draft, its main tab as the draft manifest has it, every prefab
// rendered in its draft and every graph run in its draft. Its own
// commands and queries are draft.<appId>.<name> on the live core, decided
// and answered by the draft's graphs and sent for real, so the authors
// try the app together on its records. Nothing is installed.

// draftPage is the page name of an app's draft run: <appId>/draft.
const draftPage = "draft"

// draftHeld is the draft run's own query of the draft (app.describe), so
// a change to the manifest renders the page again.
const draftHeld = "__draft"

// draftReach is who may run a draft: its authors.
func draftReach(app string) []ui.Reach {
	return []ui.Reach{{Action: "app.write", Resource: "app:" + app}}
}

// draftShape is what a draft run's session is opened with and cannot
// follow while open: its commands' and queries' names and its frame. When
// it changes, the session's next event opens it again.
func draftShape(m apps.Manifest) string {
	var names []string
	for _, c := range m.Commands {
		names = append(names, "c:"+c.Type)
	}
	for _, q := range m.Queries {
		names = append(names, "q:"+q.Name)
	}
	slices.Sort(names)
	return m.Frame + "|" + strings.Join(names, ",")
}

// draftPageOf is the draft run of an app; mine is false when the tenant
// has no draft of it.
func draftPageOf(args features.ServiceArgs, d Deps, app string) (ui.Page, bool, error) {
	tenant := args.Principal.TenantID
	dr, ok := manifests.GetDraft(args.Context(), d.Projections, tenant, app)
	if !ok {
		return ui.Page{}, false, nil
	}
	m0 := dr.Manifest
	shape := draftShape(m0)
	page := ui.Page{
		Descriptor: extensions.Descriptor{ID: app + "/" + draftPage, Source: "apps"}, App: app, Title: m0.Name + " (draft)", Description: m0.Description,
		Reach: draftReach(app), Drafts: true,
		AppCommands: map[string]string{}, AppQueries: map[string]string{},
		Queries: func(json.RawMessage) map[string]ui.QueryRef {
			return map[string]ui.QueryRef{draftHeld: {Query: "app.describe", Params: map[string]any{"id": app}}}
		},
		Stale: func() bool {
			cur, ok := manifests.GetDraft(context.Background(), d.Projections, tenant, app)
			return !ok || draftShape(cur.Manifest) != shape
		},
	}
	for _, c := range m0.Commands {
		page.AppCommands[c.Type] = apps.DraftScoped(app, c.Type)
	}
	for _, q := range m0.Queries {
		page.AppQueries[q.Name] = apps.DraftScoped(app, q.Name)
	}
	src := manifestOf{
		render: func(r *ui.Render) apps.Manifest {
			raw, _ := json.Marshal(r.Query(draftHeld, "manifest"))
			var m apps.Manifest
			if json.Unmarshal(raw, &m) != nil || m.ID == "" {
				return m0 // not read yet
			}
			apps.Normalize(&m)
			return m
		},
		handle: func(h *ui.Handle) apps.Manifest {
			if cur, ok := manifests.GetDraft(h.Context(), d.Projections, tenant, app); ok {
				return cur.Manifest
			}
			return m0
		},
		suffix: " (draft)",
	}
	frame, declared := frameOf(args, d, m0.Frame, nil, true)
	return mainPage(page, src, frame, declared), true, nil
}

// draftGraph is the draft of the graph the app's draft names for one of
// its commands (command) or queries, and the draft's item.
func draftGraph(args features.ServiceArgs, d Deps, app, name string, command bool) (cognition.Graph, bool) {
	dr, ok := manifests.GetDraft(args.Context(), d.Projections, args.Principal.TenantID, app)
	if !ok || d.Nodes.Engine == nil {
		return cognition.Graph{}, false
	}
	id, kind := "", livenodes.KindQuery
	if command {
		kind = livenodes.KindCommand
		if i := slices.IndexFunc(dr.Manifest.Commands, func(c apps.Command) bool { return c.Type == name }); i >= 0 {
			id = dr.Manifest.Commands[i].Graph
		}
	} else if i := slices.IndexFunc(dr.Manifest.Queries, func(q apps.Query) bool { return q.Name == name }); i >= 0 {
		id = dr.Manifest.Queries[i].Graph
	}
	if id == "" {
		return cognition.Graph{}, false
	}
	g, ok := d.Nodes.Engine.Draft(args, id)
	return g, ok && g.Kind == kind
}

// draftCommand is a command of an app's draft run, draft.<appId>.<type>:
// sent only by the app's authors and decided by the draft of its command
// graph as it is when the command is decided.
func draftCommand(args features.ServiceArgs, d Deps, typ, app, name string) (live.Command, bool) {
	g, ok := draftGraph(args, d, app, name, true)
	if !ok {
		return live.Command{}, false
	}
	spec, _ := specOf[livenodes.CommandSpec](g.Spec)
	return live.Command{
		Descriptor: extensions.Descriptor{ID: typ, Source: "appDraft:" + app}, Title: spec.Title, Description: spec.Description,
		Args: spec.Args, Events: spec.Events, Area: live.Area(spec.Area), TargetType: spec.TargetType, Projection: spec.Projection,
		Action: "app.write", Resource: func(p features.Principal, _ live.Target) string { return apps.Resource(p, app) },
		Decide: func(dec live.Decision) ([]live.NewEvent, []live.Problem) {
			g, ok := draftGraph(dec.ServiceArgs, d, app, name, true)
			if !ok {
				return nil, []live.Problem{{Code: "app_command_removed", Message: fmt.Sprintf("the draft of %s has no command %s now", app, name)}}
			}
			return livenodes.Decide(d.Nodes, dec, typ, g.ID, &g)
		},
	}, true
}

// draftQuery is a query of an app's draft run, draft.<appId>.<name>: read
// only by the app's authors, answered by the draft of its query graph and
// run again whenever that draft changes.
func draftQuery(args features.ServiceArgs, d Deps, typ, app, name string) (live.Query, bool) {
	g, ok := draftGraph(args, d, app, name, false)
	if !ok {
		return live.Query{}, false
	}
	spec, _ := specOf[livenodes.QuerySpec](g.Spec)
	return live.Query{
		Descriptor: extensions.Descriptor{ID: typ, Source: "appDraft:" + app}, Title: spec.Title, Description: spec.Description,
		Params: spec.Params, Action: "app.write", Resource: func(p features.Principal, _ json.RawMessage) string { return apps.Resource(p, app) },
		Run: func(f live.Feed) error {
			return livenodes.RunQuery(f, d.Nodes, livenodes.QueryRun{ID: g.ID,
				Graph: func() (*cognition.Graph, error) {
					g, ok := draftGraph(f.Args(), d, app, name, false)
					if !ok {
						msg := fmt.Sprintf("the draft of %s has no query %s now", app, name)
						return nil, log.Code("app_query_removed").Public(msg).New(msg)
					}
					return &g, nil
				},
				Watch: livenodes.Read{Projector: drafts.DraftProjector, Key: drafts.TargetType + "." + g.ID},
			})
		},
	}, true
}
