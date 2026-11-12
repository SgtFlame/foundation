package impl

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"gorm.io/gorm"
)

// Export writes a published version as one document: its manifest, the
// versions it pinned of its prefabs and entity types and its copies of
// graphs, the entity types those name, and with Seed the records of its
// own entity types the principal may read; then runs the check and
// refuses a document that fails it.
func (s *service) Export(a appservice.ExportArgs) appservice.ExportResponse {
	if fail := check(a.ServiceArgs, "app.read", apps.Resource(a.Principal, a.App)); fail != nil {
		return appservice.ExportResponse{Common: *fail}
	}
	d := s.deps()
	v, ok := manifests.GetVersion(a.ServiceArgs, d.Projections, a.App, a.Version)
	if !ok {
		err := log.Code("app_version_not_found").With("appId", a.App).With("version", a.Version).Errorf("the app %q has no published version %d", a.App, a.Version)
		return appservice.ExportResponse{Common: *a.Fail(http.StatusNotFound, err)}
	}
	x := &exporter{args: a.ServiceArgs, d: d, v: v, absent: map[string]bool{}}
	doc, err := x.document(a.Seed)
	if err != nil {
		return appservice.ExportResponse{Common: *a.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if problems := apps.CheckDocument(doc, env(d.Registry)); problems != nil {
		err := log.Code("app_check_failed").With("appId", a.App).Errorf("the app does not pass the check: %s", problems[0].Message)
		return appservice.ExportResponse{Common: *a.Fail(http.StatusUnprocessableEntity, err), Problems: problems}
	}
	return appservice.ExportResponse{Common: features.OK(), Document: &doc}
}

// Check runs the check on a document against this server's primitives.
func (s *service) Check(a appservice.CheckArgs) appservice.CheckResponse {
	if fail := check(a.ServiceArgs, "app.read", apps.Resource(a.Principal, "")); fail != nil {
		return appservice.CheckResponse{Common: *fail}
	}
	doc := a.Document
	doc.Normalize()
	problems := apps.CheckDocument(doc, env(s.deps().Registry))
	if problems == nil {
		problems = []live.Problem{}
	}
	return appservice.CheckResponse{Common: features.OK(), Problems: problems}
}

type exporter struct {
	args features.ServiceArgs
	d    Deps
	v    apps.Version
	doc  apps.Document
	// absent are entity types named but never published, which the check
	// names
	absent map[string]bool
}

func (x *exporter) document(seed bool) (apps.Document, error) {
	v := x.v
	m := v.Manifest
	m.Version = 0 // the document's version says which
	x.doc = apps.Document{Format: apps.DocumentFormat, App: v.App, Version: v.Version, PublishedAt: v.PublishedAt,
		ExportedAt: time.Now().UTC(), Manifest: m}
	x.doc.Normalize()
	for _, id := range slices.Sorted(maps.Keys(v.Prefabs)) {
		p, row, err := prefabs.Version(x.args, prefabs.Deps{Projections: x.d.Projections, DB: x.d.DB}, id, v.Prefabs[id])
		if err != nil {
			return x.doc, err
		}
		x.group(p.Owner).Prefabs = append(x.group(p.Owner).Prefabs, apps.PrefabPart{ID: id, Version: row.Version, PublishedAt: row.PublishedAt, Prefab: p})
	}
	for _, id := range slices.Sorted(maps.Keys(v.Graphs)) {
		g := v.Graphs[id]
		if id == "" || g.ID == "" {
			continue
		}
		if _, drafted := part(x.args, x.d, apps.PartGraph, id); !drafted {
			if _, ok := env(x.d.Registry).Builtin(id); ok {
				x.doc.Primitives.Graphs = append(x.doc.Primitives.Graphs, id)
				continue
			}
		}
		x.group(g.Owner).Graphs = append(x.group(g.Owner).Graphs, apps.GraphPart{ID: id, Version: g.Version, PublishedAt: v.PublishedAt, Graph: g})
	}
	for _, id := range slices.Sorted(maps.Keys(v.Entities)) {
		if err := x.entity(id, v.Entities[id]); err != nil {
			return x.doc, err
		}
	}
	// The entity types the parts name beyond the manifest's, at their last
	// published version
	for {
		missing := x.missingEntities()
		if len(missing) == 0 {
			break
		}
		for _, id := range missing {
			if err := x.entity(id, 0); err != nil {
				return x.doc, err
			}
		}
	}
	used := apps.Used(x.doc, env(x.d.Registry))
	x.doc.Primitives.Components, x.doc.Primitives.Nodes = used.Components, used.Nodes
	if seed {
		if err := x.seed(); err != nil {
			return x.doc, err
		}
	}
	return x.doc, nil
}

// group is the document's group for a part's owner.
func (x *exporter) group(owner string) *apps.Parts {
	switch {
	case owner == apps.Owner(x.v.App):
		return &x.doc.Parts
	case strings.HasPrefix(owner, "app:"):
		return &x.doc.AppParts
	}
	return &x.doc.TenantParts
}

// entity adds a published entity type version; 0 is its last.
func (x *exporter) entity(id string, version int) error {
	var row records.EntityTypeVersion
	q := x.d.DB.Model(&records.EntityTypeVersion{}).Scopes(db.InTenant(x.args.Principal.TenantID)).Where("entity_id = ?", id)
	if version > 0 {
		q = q.Where("version = ?", version)
	}
	err := q.Order("version DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		x.absent[id] = true
		return nil
	}
	if err != nil {
		return err
	}
	var e records.EntityType
	if err := json.Unmarshal([]byte(row.Definition), &e); err != nil {
		return fmt.Errorf("the entity type %s cannot be read: %w", id, err)
	}
	g := x.group(e.Owner)
	g.Entities = append(g.Entities, apps.EntityPart{ID: id, Version: row.Version, PublishedAt: row.PublishedAt, Entity: e})
	return nil
}

// missingEntities are the entity types the document's parts name that it
// does not have.
func (x *exporter) missingEntities() []string {
	have := map[string]bool{records.UserEntity: true, "": true}
	for id := range x.absent {
		have[id] = true
	}
	var all []apps.Parts
	for _, p := range []apps.Parts{x.doc.Parts, x.doc.TenantParts, x.doc.AppParts} {
		all = append(all, p)
		for _, e := range p.Entities {
			have[e.ID] = true
		}
	}
	var out []string
	add := func(id string) {
		if !have[id] {
			have[id] = true
			out = append(out, id)
		}
	}
	for _, p := range all[:2] {
		for _, pr := range p.Prefabs {
			for _, k := range slices.Sorted(maps.Keys(pr.Prefab.Props)) {
				add(pr.Prefab.Props[k].Entity)
			}
			ui.WalkTemplate(pr.Prefab.Template, func(_ []string, n *ui.Node) {
				if e, ok := n.Props["entity"].(string); ok {
					add(e)
				}
			})
		}
		for _, g := range p.Graphs {
			for _, n := range g.Graph.Nodes {
				var c struct {
					Entity any `json:"entity"`
				}
				if json.Unmarshal(n.Config, &c) == nil {
					if e, ok := c.Entity.(string); ok {
						add(e)
					}
				}
			}
		}
		for _, e := range p.Entities {
			for _, f := range e.Entity.Fields {
				if f.Relation != nil {
					add(f.Relation.Entity)
				}
			}
		}
	}
	return out
}

// seed adds the records of the app's own entity types the principal may
// read, with the values a command may set: no computed fields, no files
// (their content is not exported) and no removed fields.
func (x *exporter) seed() error {
	out := apps.Seed{}
	for _, e := range x.doc.Parts.Entities {
		params, _ := json.Marshal(map[string]any{"entity": e.ID})
		raw, _, fail := x.d.Subscriptions.Once(x.args, "records.list", params)
		if fail != nil {
			return fmt.Errorf("the records of %s cannot be read: %s", e.ID, fail.Message)
		}
		b, _ := json.Marshal(raw)
		var byKey map[string]data.Record
		if err := json.Unmarshal(b, &byKey); err != nil {
			return err
		}
		var recs []apps.SeedRecord
		for _, r := range byKey {
			var id string
			if json.Unmarshal(r[data.MetaID], &id) != nil {
				continue
			}
			sr := apps.SeedRecord{ID: id, Values: map[string]json.RawMessage{}}
			for _, f := range e.Entity.Fields {
				v, ok := r[f.ID]
				// An export never carries connections (forge 07c)
				if !ok || f.Hidden || records.IsComputed(f) || f.Type == "file" || f.Type == "connection" || string(v) == "null" {
					continue
				}
				sr.Values[f.ID] = v
			}
			recs = append(recs, sr)
		}
		// Record ids are TSIDs, so a longer id is a later one
		slices.SortFunc(recs, func(a, b apps.SeedRecord) int {
			if len(a.ID) != len(b.ID) {
				return len(a.ID) - len(b.ID)
			}
			return strings.Compare(a.ID, b.ID)
		})
		if len(recs) > 0 {
			out[e.ID] = recs
		}
	}
	if len(out) > 0 {
		x.doc.Seed = out
	}
	return nil
}
