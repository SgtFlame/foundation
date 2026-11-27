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

// onBranch is args reading definitions on branch b (forge 19b).
func onBranch(args features.ServiceArgs, b live.Branch) features.ServiceArgs {
	args.Ctx = live.WithBranch(args.Context(), b)
	return args
}

// branchName is a branch's name, "" for main.
func branchName(b live.Branch) string {
	if b.IsMain() {
		return ""
	}
	return b.Name
}

// draftPageOf is the draft run of an app on branch b, <appId>/draft on
// main and <appId>/draft@<branch> on a branch (forge 19b): the manifest,
// prefabs and graphs as the branch has them. mine is false when the
// tenant has no draft of it there.
func draftPageOf(args features.ServiceArgs, d Deps, b live.Branch) (ui.Page, bool, error) {
	app := b.App
	tenant := args.Principal.TenantID
	args = onBranch(args, b)
	store := live.StoreFor(args.Context(), d.Projections)
	dr, ok := manifests.GetDraft(args.Context(), store, tenant, app)
	if !ok {
		return ui.Page{}, false, nil
	}
	m0 := dr.Manifest
	shape := draftShape(m0)
	id, suffix, describe := app+"/"+draftPage, " (draft)", map[string]any{"id": app}
	if !b.IsMain() {
		id, suffix = id+"@"+b.Name, " (draft on "+b.Name+")"
		describe["branch"] = b.String()
	}
	page := ui.Page{
		Descriptor: extensions.Descriptor{ID: id, Source: "apps"}, App: app, Title: m0.Name + suffix, Description: m0.Description,
		Reach: draftReach(app), Drafts: true, Branch: b,
		AppCommands: map[string]string{}, AppQueries: map[string]string{},
		Queries: func(json.RawMessage) map[string]ui.QueryRef {
			return map[string]ui.QueryRef{draftHeld: {Query: "app.describe", Params: describe}}
		},
		Stale: func() bool {
			cur, ok := manifests.GetDraft(context.Background(), store, tenant, app)
			return !ok || draftShape(cur.Manifest) != shape
		},
	}
	for _, c := range m0.Commands {
		page.AppCommands[c.Type] = apps.DraftBranchScoped(branchName(b), app, c.Type)
	}
	for _, q := range m0.Queries {
		page.AppQueries[q.Name] = apps.DraftBranchScoped(branchName(b), app, q.Name)
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
			if cur, ok := manifests.GetDraft(h.Context(), store, tenant, app); ok {
				return cur.Manifest
			}
			return m0
		},
		suffix: suffix,
	}
	frame, declared := frameOf(args, d, m0.Frame, nil, true)
	return mainPage(page, src, frame, declared), true, nil
}

// draftGraph is the draft of the graph the app's draft names for one of
// its commands (command) or queries, and the draft's item.
func draftGraph(args features.ServiceArgs, d Deps, b live.Branch, name string, command bool) (cognition.Graph, bool) {
	app := b.App
	args = onBranch(args, b)
	dr, ok := manifests.GetDraft(args.Context(), live.StoreFor(args.Context(), d.Projections), args.Principal.TenantID, app)
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
func draftCommand(args features.ServiceArgs, d Deps, typ string, b live.Branch, name string) (live.Command, bool) {
	app := b.App
	g, ok := draftGraph(args, d, b, name, true)
	if !ok {
		return live.Command{}, false
	}
	spec, _ := specOf[livenodes.CommandSpec](g.Spec)
	return live.Command{
		Descriptor: extensions.Descriptor{ID: typ, Source: "appDraft:" + app}, Title: spec.Title, Description: spec.Description,
		Args: spec.Args, Events: spec.Events, Area: live.Area(spec.Area), TargetType: spec.TargetType, Projection: spec.Projection,
		Action: "app.write", Resource: func(p features.Principal, _ live.Target) string { return apps.Resource(p, app) },
		Decide: func(dec live.Decision) ([]live.NewEvent, []live.Problem) {
			g, ok := draftGraph(dec.ServiceArgs, d, b, name, true)
			if !ok {
				return nil, []live.Problem{{Code: "app_command_removed", Message: fmt.Sprintf("the draft of %s has no command %s now", app, name)}}
			}
			dec.ServiceArgs = onBranch(dec.ServiceArgs, b)
			return livenodes.Decide(d.Nodes, dec, typ, g.ID, &g)
		},
	}, true
}

// draftQuery is a query of an app's draft run, draft.<appId>.<name>: read
// only by the app's authors, answered by the draft of its query graph and
// run again whenever that draft changes.
func draftQuery(args features.ServiceArgs, d Deps, typ string, b live.Branch, name string) (live.Query, bool) {
	app := b.App
	g, ok := draftGraph(args, d, b, name, false)
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
					g, ok := draftGraph(f.Args(), d, b, name, false)
					if !ok {
						msg := fmt.Sprintf("the draft of %s has no query %s now", app, name)
						return nil, log.Code("app_query_removed").Public(msg).New(msg)
					}
					return &g, nil
				},
				// On a branch, its draft there: made by the first change
				Watch: livenodes.Read{Projector: drafts.DraftProjector, Key: drafts.TargetType + "." + live.BranchTargetID(g.ID, b)},
			})
		},
	}, true
}
