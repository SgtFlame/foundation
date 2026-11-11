package manifests

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// DraftDocument is an app's draft as one document (forge 06e), as
// app.export would write it were the draft published: the draft manifest,
// the drafts (else last published versions) of every part it names or
// owns and of every part those use, by owner, and the primitives they
// use. A part named but not there is left out, for the check to name.
func DraftDocument(ctx context.Context, store live.Store, tenant uint64, reg *extensions.Registry, app string) (apps.Document, bool) {
	d, ok := GetDraft(ctx, store, tenant, app)
	if !ok {
		return apps.Document{}, false
	}
	m := d.Manifest
	m.Version = 0
	doc := apps.Document{Format: apps.DocumentFormat, App: app, Manifest: m}
	env := Env(ctx, store, tenant, reg)
	denv := apps.DocumentEnvOf(reg)
	group := func(owner string) *apps.Parts {
		switch {
		case owner == apps.Owner(app):
			return &doc.Parts
		case strings.HasPrefix(owner, "app:"):
			return &doc.AppParts
		}
		return &doc.TenantParts
	}
	seen := map[string]bool{}
	var queue []string // <type>:<id>
	add := func(typ, id string) {
		k := typ + ":" + id
		if id != "" && !seen[k] && !(typ == "entity" && id == records.UserEntity) {
			seen[k] = true
			queue = append(queue, k)
		}
	}
	for typ, ids := range named(m) {
		for _, id := range ids {
			add(typ, id)
		}
	}
	for _, typ := range []string{"prefab", "graph", "entity"} {
		for _, id := range ownedParts(ctx, store, tenant, typ, apps.Owner(app)) {
			add(typ, id)
		}
	}
	slices.Sort(queue)
	for len(queue) > 0 {
		typ, id, _ := strings.Cut(queue[0], ":")
		queue = queue[1:]
		switch typ {
		case "prefab":
			p, ok := env.Prefab(id)
			if !ok {
				continue
			}
			g := group(p.Owner)
			g.Prefabs = append(g.Prefabs, apps.PrefabPart{ID: id, Prefab: p})
			for _, used := range ui.PrefabsUsed(p.Template) {
				add("prefab", used)
			}
			for _, graph := range p.Graphs {
				add("graph", graph)
			}
			for _, pr := range p.Props {
				add("entity", pr.Entity)
			}
			ui.WalkTemplate(p.Template, func(_ []string, n *ui.Node) {
				if e, ok := n.Props["entity"].(string); ok {
					add("entity", e)
				}
			})
		case "graph":
			_, drafted := get(ctx, store, tenant, drafts.DraftProjector, drafts.TargetType+"."+id)
			_, published := get(ctx, store, tenant, drafts.PublishedProjector, drafts.TargetType+"."+id)
			if _, builtin := denv.Builtin(id); builtin && !drafted && !published {
				doc.Primitives.Graphs = append(doc.Primitives.Graphs, id)
				continue
			}
			gr, ok := env.Graph(id)
			if !ok {
				continue
			}
			g := group(gr.Owner)
			g.Graphs = append(g.Graphs, apps.GraphPart{ID: id, Graph: gr})
			for _, n := range gr.Nodes {
				var config struct {
					Entity any `json:"entity"`
				}
				if json.Unmarshal(n.Config, &config) == nil {
					if e, ok := config.Entity.(string); ok {
						add("entity", e)
					}
				}
			}
		case "entity":
			e, ok := env.Entity(id)
			if !ok {
				var pub records.EntityType
				raw, found := get(ctx, store, tenant, entities.PublishedProjector, entities.TargetType+"."+id)
				if !found || json.Unmarshal(raw, &pub) != nil {
					continue
				}
				e = pub
			}
			g := group(e.Owner)
			g.Entities = append(g.Entities, apps.EntityPart{ID: id, Entity: e})
			for _, f := range e.Fields {
				if f.Relation != nil {
					add("entity", f.Relation.Entity)
				}
			}
		}
	}
	doc.Normalize()
	used := apps.Used(doc, denv)
	doc.Primitives.Components, doc.Primitives.Nodes = used.Components, used.Nodes
	if doc.Primitives.Graphs == nil {
		doc.Primitives.Graphs = []string{}
	}
	return doc, true
}

// ownedParts are the ids of the parts of a type an owner has, deleted
// ones left out.
func ownedParts(ctx context.Context, store live.Store, tenant uint64, typ, owner string) []string {
	projector, target := prefabs.DraftProjector, prefabs.TargetType
	switch typ {
	case "graph":
		projector, target = drafts.DraftProjector, drafts.TargetType
	case "entity":
		projector, target = entities.DraftProjector, entities.TargetType
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := store.Watch(ctx, tenant, projector, target+".*")
	if err != nil {
		return nil
	}
	var out []string
	for e := range ch {
		if e == nil {
			break
		}
		var deleted struct{ Deleted bool }
		_ = json.Unmarshal(e.Value.Value, &deleted)
		if p, ok := partOf(typ, e.Value.Value); ok && !e.Deleted && !deleted.Deleted && p.Owner == owner {
			out = append(out, p.ID)
		}
	}
	return out
}

// CheckDraft is forge.check (forge 06e): the errors of an app's draft,
// those of validation and of the blueprints-only check, as the draft
// would be exported (DraftDocument, apps.CheckDocument); and its
// warnings: pages nothing reaches, parts of the app nothing uses, and
// parts and pages with no description for a model (forge 06d question 2).
func CheckDraft(ctx context.Context, store live.Store, tenant uint64, reg *extensions.Registry, app string) (errors, warnings []live.Problem, ok bool) {
	doc, ok := DraftDocument(ctx, store, tenant, reg, app)
	if !ok {
		return nil, nil, false
	}
	errors = apps.CheckDocument(doc, apps.DocumentEnvOf(reg))
	if errors == nil {
		errors = []live.Problem{}
	}
	warnings = append(Lint(doc), apps.ConnectorWarnings(doc.Manifest, apps.BuiltinContracts(reg))...)
	return errors, warnings, true
}

// Lint is the warnings of an app document: what does not stop it running
// but a builder should look at.
func Lint(doc apps.Document) []live.Problem {
	m := doc.Manifest
	out := []live.Problem{}
	warn := func(path, message, fix string) {
		out = append(out, live.Problem{Code: "warning", Path: path, Message: message, Fix: fix})
	}
	// The JSON of the parts the manifest reaches, grown until no more
	// are found: a part is reached when it is named, or when a reached
	// part names it
	type part struct{ typ, id, owner, description, text string }
	var all []part
	for _, g := range []apps.Parts{doc.Parts, doc.TenantParts, doc.AppParts} {
		for _, p := range g.Prefabs {
			raw, _ := json.Marshal(p.Prefab)
			all = append(all, part{"prefab", p.ID, p.Prefab.Owner, p.Prefab.Description, string(raw)})
		}
		for _, p := range g.Graphs {
			raw, _ := json.Marshal(p.Graph)
			all = append(all, part{"graph", p.ID, p.Graph.Owner, p.Graph.Description, string(raw)})
		}
		for _, p := range g.Entities {
			raw, _ := json.Marshal(p.Entity)
			all = append(all, part{"entity", p.ID, p.Entity.Owner, p.Entity.Description, string(raw)})
		}
	}
	reached := map[string]bool{}
	for typ, ids := range named(m) {
		for _, id := range ids {
			reached[typ+":"+id] = true
		}
	}
	// The manifest itself, whose triggers name graphs in their config
	raw, _ := json.Marshal(m)
	all = append(all, part{typ: "manifest", text: string(raw)})
	reached["manifest:"] = true
	names := func(text string, p part) bool {
		if p.typ == "prefab" {
			return strings.Contains(text, `"prefab.`+p.id+`"`) || strings.Contains(text, `"prefab.`+p.id+`@`)
		}
		return strings.Contains(text, `"`+p.id+`"`)
	}
	for grown := true; grown; {
		grown = false
		for _, p := range all {
			if reached[p.typ+":"+p.id] {
				continue
			}
			for _, q := range all {
				if reached[q.typ+":"+q.id] && names(q.text, p) {
					reached[p.typ+":"+p.id], grown = true, true
					break
				}
			}
		}
	}
	own := apps.Owner(doc.App)
	for _, p := range all {
		if p.typ == "manifest" || p.owner != own {
			continue
		}
		path := fmt.Sprintf("parts.%s[%s]", p.typ, p.id)
		if !reached[p.typ+":"+p.id] {
			warn(path, fmt.Sprintf("nothing in the app uses the %s %s", p.typ, p.id), "use it from a page, command or query, or delete it if it is left over")
		}
		if p.description == "" {
			warn(path+".description", fmt.Sprintf("the %s %s has no description", p.typ, p.id), "describe what it is for in a sentence, for the next author and the model")
		}
	}
	// Pages: reached by the nav, as home, by an editor kind, or by a part
	// that names its route (up to its first param)
	var texts []string
	for _, p := range all {
		if p.typ != "manifest" && reached[p.typ+":"+p.id] {
			texts = append(texts, p.text)
		}
	}
	text := strings.Join(texts, "\n")
	for _, pg := range m.Pages {
		path := "manifest.pages[" + pg.Route + "]"
		fixed, _, _ := strings.Cut(pg.Route, ":")
		_, kind := m.KindFor(pg.Route)
		inNav := slices.ContainsFunc(m.Nav, func(n apps.NavItem) bool { return n.Route == pg.Route })
		if !inNav && !kind && pg.Route != m.HomeRoute() && !strings.Contains(text, `"`+fixed) {
			warn(path, fmt.Sprintf("nothing reaches the page %s: it is not home, in the nav or an editor kind, and no part navigates to it", pg.Route), "add it to the nav with app.setNav, or navigate to it from a page")
		}
		if pg.Description == "" {
			warn(path+".description", fmt.Sprintf("the page %s has no description", pg.Route), "set it with app.updatePage {route, description}: what the page shows and who it is for")
		}
	}
	return out
}
