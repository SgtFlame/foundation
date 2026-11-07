package manifests

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

func publishSpec() spec {
	return spec{
		id: "app.publish", title: "Publish an app", events: []string{EventPublished}, action: "app.publish",
		description: "Publishes the app as a new version, which installs and upgrades take: the manifest as it was at rev (the draft's rev its publisher last saw; changes others made since stay in the draft for the next version), with the published version of every part it uses pinned. Every page's prefab, the frame, the prefabs they render, the command and query graphs and the entity types must be published, and are checked as published; a part published again later reaches the app's users only by publishing the app again and upgrading.",
		args:        `{"type":"object","properties":{"rev":{"type":"integer","description":"The draft's rev as last seen (app.* results and app.describe give it)"}},"required":["rev"]}`,
		example:     `{"rev":14}`,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := args[struct{ Rev int }](c)
			if problems != nil {
				return nil, problems
			}
			if a.Rev < 1 || a.Rev > c.draft.Rev {
				return nil, problem("args.rev", fmt.Sprintf("the draft has no rev %d: it is at rev %d", a.Rev, c.draft.Rev), "publish rev "+strconv.Itoa(c.draft.Rev))
			}
			m, err := c.manifestAt(a.Rev)
			if err != nil {
				return nil, problem("args.rev", "the draft at that rev cannot be read: "+err.Error(), "publish rev "+strconv.Itoa(c.draft.Rev))
			}
			v, problems := c.pin(m)
			if problems != nil {
				return nil, problems
			}
			// The versions' projection and their copy into the tenant
			// database run before the event lands
			if _, _, err := c.deps.Projections.Get(c.Context(), c.Principal.TenantID, VersionProjector, VersionTargetType+".-"); err != nil {
				return nil, problem("", "the published versions cannot be kept: "+err.Error(), "")
			}
			if err := FollowVersions(c.ServiceArgs, c.deps); err != nil {
				return nil, problem("", "the published versions cannot be kept: "+err.Error(), "")
			}
			v.App, v.Version, v.Rev = c.Target.ID, c.draft.Published+1, a.Rev
			v.Manifest.Version = v.Version
			v.PublishedBy, v.PublishedAt = c.Principal.UserID, time.Now().UTC()
			target := live.Target{Area: live.AreaDefinitions, Type: VersionTargetType, ID: apps.VersionID(v.App, v.Version)}
			return []live.NewEvent{
				{Type: EventPublished, Data: Published{Version: v.Version, Rev: a.Rev}},
				{Type: EventPublished, Data: v, Target: &target},
			}, nil
		},
	}
}

// manifestAt is the manifest as it was at rev: the draft's own when rev
// is its last, else its events folded again up to rev.
func (c *decision) manifestAt(rev int) (apps.Manifest, error) {
	if rev == c.draft.Rev {
		return c.draft.Manifest, nil
	}
	var d Draft
	var failed error
	err := c.deps.Projections.History(c.Context(), c.Principal.TenantID, c.Target, func(e live.Event) bool {
		if failed = Apply(&d, e); failed != nil {
			return false
		}
		return d.Rev < rev
	})
	if err == nil {
		err = failed
	}
	if err == nil && d.Rev != rev {
		err = fmt.Errorf("only %d events were found", d.Rev)
	}
	return d.Manifest, err
}

// published reads a part's last published version.
func (c *decision) published(projector, key string, v any) bool {
	raw, ok := get(c.Context(), c.deps.Projections, c.Principal.TenantID, projector, key)
	return ok && json.Unmarshal(raw, v) == nil
}

// publishedGraph is a graph as it runs: its published version, else the
// built-in.
func (c *decision) publishedGraph(id string) (cognition.Graph, bool) {
	var g cognition.Graph
	if live.ValidToken(id) && c.published(drafts.PublishedProjector, drafts.TargetType+"."+id, &g) {
		return g, true
	}
	built := cognition.Graphs.All(c.reg)
	if i := slices.IndexFunc(built, func(b cognition.BuiltIn) bool { return b.ID == id }); i >= 0 {
		return built[i].Graph, true
	}
	return cognition.Graph{}, false
}

func (c *decision) publishedPrefab(id string) (ui.Prefab, bool) {
	var p ui.Prefab
	return p, live.ValidToken(id) && c.published(prefabs.PublishedProjector, prefabs.TargetType+"."+id, &p)
}

func (c *decision) publishedEntity(id string) (records.EntityType, bool) {
	var e records.EntityType
	ok := live.ValidToken(id) && c.published(entities.PublishedProjector, entities.TargetType+"."+id, &e)
	return e, ok && !e.Hidden
}

// pin checks a manifest against the published versions of its parts and
// pins them: the version a publish would freeze, or the problems that
// stop it.
func (c *decision) pin(m apps.Manifest) (apps.Version, []live.Problem) {
	v := apps.Version{Manifest: m, Prefabs: map[string]int{}, Entities: map[string]int{}, Graphs: map[string]cognition.Graph{}}
	var problems []live.Problem
	unpublished := func(path, what, fix string) {
		problems = append(problems, live.Problem{Code: "app_unpublished", Path: path, Message: what + " is not published", Fix: fix})
	}
	// Parts not published yet are named first, plainly
	for _, n := range named(m)["prefab"] {
		if _, ok := c.publishedPrefab(n); !ok {
			unpublished("parts.prefab["+n+"]", "the prefab "+n, "publish it with prefab.publish")
		}
	}
	for _, n := range named(m)["graph"] {
		if _, ok := c.publishedGraph(n); !ok {
			unpublished("parts.graph["+n+"]", "the graph "+n, "publish it with bp.publish")
		}
	}
	for _, n := range named(m)["entity"] {
		if _, ok := c.publishedEntity(n); !ok {
			unpublished("parts.entity["+n+"]", "the entity type "+n, "publish it with entity.publish")
		}
	}
	if problems != nil {
		return v, problems
	}
	env := apps.Env{Catalog: ui.Catalog(c.reg), Prefab: c.publishedPrefab, Graph: c.publishedGraph, Entity: c.publishedEntity}
	for _, p := range apps.CheckManifest(m, env) {
		p.Path = strings.TrimSuffix("manifest."+p.Path, ".")
		problems = append(problems, p)
	}
	if problems != nil {
		return v, problems
	}

	// Every prefab the pages and frame render, and those they render
	var queue []string
	if id, ok := strings.CutPrefix(m.Frame, "prefab."); ok {
		queue = append(queue, id)
	}
	for _, p := range m.Pages {
		queue = append(queue, p.Prefab)
	}
	used := map[string]ui.Prefab{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, seen := used[id]; seen {
			continue
		}
		p, ok := c.publishedPrefab(id)
		if !ok {
			unpublished("parts.prefab["+id+"]", "the prefab "+id+", which the app's pages render,", "publish it with prefab.publish")
			continue
		}
		used[id] = p
		v.Prefabs[id] = p.Version
		queue = append(queue, ui.PrefabsUsed(p.Template)...)
	}
	// The instances in its pages are checked as published (left by forge 4a)
	penv := c.prefabEnv(m)
	for id, p := range used {
		for _, pr := range ui.CheckPrefab(p, penv) {
			pr.Path = "parts.prefab[" + id + "]." + pr.Path
			problems = append(problems, pr)
		}
		for _, g := range p.Graphs {
			if pg, ok := c.publishedGraph(g); ok {
				v.Graphs[g] = pg
			}
		}
	}
	for _, cmd := range m.Commands {
		v.Graphs[cmd.Graph], _ = c.publishedGraph(cmd.Graph)
	}
	for _, q := range m.Queries {
		v.Graphs[q.Graph], _ = c.publishedGraph(q.Graph)
	}
	for _, e := range m.Entities {
		et, _ := c.publishedEntity(e)
		v.Entities[e] = et.Version
		if et.Owner == apps.Owner(m.ID) {
			v.Owned = append(v.Owned, e)
		}
	}
	slices.SortFunc(problems, func(a, b live.Problem) int { return strings.Compare(a.Path, b.Path) })
	return v, problems
}

// prefabEnv checks published prefabs against published ones only.
func (c *decision) prefabEnv(m apps.Manifest) ui.PrefabEnv {
	return ui.PrefabEnv{
		Catalog: ui.Catalog(c.reg),
		Prefab:  c.publishedPrefab,
		Uses: func(id string) []string {
			p, _ := c.publishedPrefab(id)
			return ui.PrefabsUsed(p.Template)
		},
		Version: func(id string, version int) (ui.Prefab, bool) {
			if c.deps.DB == nil {
				return ui.Prefab{}, false
			}
			p, _, err := prefabs.Version(c.ServiceArgs, prefabs.Deps{Projections: c.deps.Projections, DB: c.deps.DB}, id, version)
			return p, err == nil
		},
		Entity: func(id string) bool { _, ok := c.publishedEntity(id); return ok },
		Query: func(name string) bool {
			if slices.ContainsFunc(m.Queries, func(q apps.Query) bool { return q.Name == name }) ||
				slices.ContainsFunc(live.Queries.All(c.reg), func(q live.Query) bool { return q.ID == name }) {
				return true
			}
			_, ok := c.publishedGraph(livenodes.QueryGraphID(name))
			return ok
		},
		Graph: func(id string) (string, bool) {
			g, ok := c.publishedGraph(id)
			return g.Kind, ok
		},
	}
}

// foldVersion is the apps.version projector's fold: a version, once
// published, never changes.
func foldVersion(f live.Fold) (json.RawMessage, error) {
	if f.Event.Type != EventPublished || f.State != nil {
		return f.State, nil
	}
	return f.Event.Data, nil
}

// GetVersion reads a published version of an app; version 0 is its last.
func GetVersion(args features.ServiceArgs, store live.Store, app string, version int) (apps.Version, bool) {
	ctx, tenant := args.Context(), args.Principal.TenantID
	raw, ok := get(ctx, store, tenant, DraftProjector, TargetType+"."+app)
	var d Draft
	if !ok || json.Unmarshal(raw, &d) != nil || d.Published == 0 || version > d.Published {
		return apps.Version{}, false
	}
	if version == 0 {
		version = d.Published
	}
	key := VersionTargetType + "." + apps.VersionID(app, version)
	raw, ok = get(ctx, store, tenant, VersionProjector, key)
	if s, settles := store.(settler); !ok && settles {
		// The draft has it: the versions' projector is catching up, as
		// after a restart
		_ = s.Settle(ctx, tenant, VersionProjector)
		raw, ok = get(ctx, store, tenant, VersionProjector, key)
	}
	var v apps.Version
	return v, ok && json.Unmarshal(raw, &v) == nil
}

// settler is a store that can wait until a projector has folded its
// stream.
type settler interface {
	Settle(ctx context.Context, tenant uint64, projector string) error
}
