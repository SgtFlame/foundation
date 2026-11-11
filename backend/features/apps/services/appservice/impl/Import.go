package impl

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// Import checks a document, plans it against the principal's tenant and,
// once every part the tenant already has is decided, recreates it by the
// commands an author would send, as the principal: the entity types,
// graphs and prefabs imported and published, then the app made, its
// manifest and sample records set and published as version 1 here. The
// app is not installed.
func (s *service) Import(a appservice.ImportArgs) appservice.ImportResponse {
	if fail := check(a.ServiceArgs, "app.write", apps.Resource(a.Principal, "")); fail != nil {
		return appservice.ImportResponse{Common: *fail}
	}
	d := s.deps()
	doc := a.Document
	doc.Normalize()
	refuse := func(status int, code, message string, problems []live.Problem, plan appservice.Plan) appservice.ImportResponse {
		err := log.Code(code).With("appId", doc.App).Errorf("%s", message)
		return appservice.ImportResponse{Common: *a.Fail(uint16(status), err), Problems: problems, Plan: plan}
	}
	if problems := apps.CheckDocument(doc, env(d.Registry)); problems != nil {
		return refuse(http.StatusUnprocessableEntity, "app_check_failed", "the document does not pass the check: "+problems[0].Message, problems, appservice.Plan{})
	}
	id := cmp.Or(a.ID, doc.App)
	if !live.ValidToken(id) {
		return refuse(http.StatusUnprocessableEntity, "app_import_id", fmt.Sprintf("%q is not an app id", id),
			[]live.Problem{{Path: "id", Message: fmt.Sprintf("%q is not an app id", id), Fix: "use letters, digits, '-' and '_'"}}, appservice.Plan{})
	}
	if id != doc.App {
		doc = renamed(doc, id)
	}
	if _, taken := get(a.Context(), d.Projections, a.Principal.TenantID, manifests.DraftProjector, manifests.TargetType+"."+id); taken {
		msg := fmt.Sprintf("this tenant already has an app %s", id)
		return refuse(http.StatusConflict, "app_import_exists", msg, []live.Problem{{Code: "app_import_exists", Path: "id", Message: msg, Fix: "import it under another id"}}, appservice.Plan{App: id})
	}
	plan, problems := s.plan(a, d, doc)
	if problems != nil {
		return refuse(http.StatusConflict, problems[0].Code, problems[0].Message, problems, plan)
	}
	if a.DryRun {
		return appservice.ImportResponse{Common: features.OK(), Plan: plan}
	}
	im := &importer{args: a.ServiceArgs, d: d, doc: doc, plan: plan, run: features.NewTSID()}
	version, err := im.importAll()
	if err != nil {
		return appservice.ImportResponse{Common: *a.Fail(http.StatusUnprocessableEntity, err), Plan: plan, Problems: im.problems}
	}
	log.Info(a.Context(), "imported the app %s as version %d", id, version)
	return appservice.ImportResponse{Common: features.OK(), Plan: plan, Imported: true, Version: version}
}

// renamed is a document whose app has another id: its own parts' owner
// and its command and query graphs' ids follow (forge 05e question 1).
func renamed(doc apps.Document, id string) apps.Document {
	raw, _ := json.Marshal(doc)
	var out apps.Document
	_ = json.Unmarshal(raw, &out)
	old, own := doc.App, apps.Owner(id)
	ids := map[string]string{}
	for i := range out.Parts.Graphs {
		g := &out.Parts.Graphs[i]
		for _, prefix := range []string{"cmd-", "qry-"} {
			if rest, ok := strings.CutPrefix(g.ID, prefix+old+"-"); ok {
				ids[g.ID] = prefix + id + "-" + rest
				g.ID, g.Graph.ID = ids[g.ID], ids[g.ID]
			}
		}
		g.Graph.Owner = own
	}
	for i := range out.Parts.Prefabs {
		out.Parts.Prefabs[i].Prefab.Owner = own
	}
	for i := range out.Parts.Entities {
		out.Parts.Entities[i].Entity.Owner = own
	}
	rename := func(g string) string { return cmp.Or(ids[g], g) }
	for _, ps := range []*apps.Parts{&out.Parts, &out.TenantParts, &out.AppParts} {
		for i := range ps.Prefabs {
			for k, g := range ps.Prefabs[i].Prefab.Graphs {
				ps.Prefabs[i].Prefab.Graphs[k] = rename(g)
			}
		}
	}
	for i := range out.Manifest.Commands {
		out.Manifest.Commands[i].Graph = rename(out.Manifest.Commands[i].Graph)
	}
	for i := range out.Manifest.Queries {
		out.Manifest.Queries[i].Graph = rename(out.Manifest.Queries[i].Graph)
	}
	out.App, out.Manifest.ID = id, id
	return out
}

// docPart is one part of a document, of any type.
type docPart struct {
	typ, id, group, name string
	version              int
	publishedAt          time.Time
	owner                string
	def                  any
}

func partsOf(doc apps.Document) []docPart {
	var out []docPart
	for _, g := range []struct {
		name string
		ps   apps.Parts
	}{{"parts", doc.Parts}, {"tenantParts", doc.TenantParts}} {
		for _, p := range g.ps.Entities {
			out = append(out, docPart{apps.PartEntity, p.ID, g.name, p.Entity.Name, p.Version, p.PublishedAt, p.Entity.Owner, p.Entity})
		}
		for _, p := range g.ps.Graphs {
			out = append(out, docPart{apps.PartGraph, p.ID, g.name, p.Graph.Name, p.Version, p.PublishedAt, p.Graph.Owner, p.Graph})
		}
		for _, p := range g.ps.Prefabs {
			out = append(out, docPart{apps.PartPrefab, p.ID, g.name, p.Prefab.Name, p.Version, p.PublishedAt, p.Prefab.Owner, p.Prefab})
		}
	}
	return out
}

// sameDefinition compares a published definition with a document's,
// their versions aside.
func sameDefinition(published json.RawMessage, def any) bool {
	var a, b map[string]any
	raw, _ := json.Marshal(def)
	if json.Unmarshal(published, &a) != nil || json.Unmarshal(raw, &b) != nil {
		return false
	}
	delete(a, "version")
	delete(b, "version")
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}

// plan says what importing each part does, and refuses with a problem
// for each part the tenant already has that the caller has not decided,
// and for each required app's part it lacks.
func (s *service) plan(a appservice.ImportArgs, d Deps, doc apps.Document) (appservice.Plan, []live.Problem) {
	plan := appservice.Plan{App: doc.App, Parts: []appservice.PartPlan{}}
	var problems []live.Problem
	for _, p := range partsOf(doc) {
		pp := appservice.PartPlan{Type: p.typ, ID: p.id, Group: p.group, Name: p.name, Status: appservice.PartNew,
			Document: appservice.PartVersion{Owner: p.owner, Version: p.version, PublishedAt: p.publishedAt}}
		if dr, ok := part(a.ServiceArgs, d, p.typ, p.id); ok {
			var def struct{ Owner string }
			_ = json.Unmarshal(dr.definition(), &def)
			pp.Target = &appservice.PartVersion{Owner: def.Owner, Version: dr.Published, UpdatedAt: dr.UpdatedAt, Changed: dr.Rev != dr.PublishedRev}
			pp.Target.PublishedAt = s.publishedAt(a.ServiceArgs, d, p.typ, p.id, dr.Published)
			pp.Status = appservice.PartDuplicate
			if pub, ok := published(a.ServiceArgs, d, p.typ, p.id); ok && sameDefinition(pub, p.def) {
				pp.Status = appservice.PartSame
			}
		}
		key := apps.PartKey(p.typ, p.id)
		switch {
		case pp.Status != appservice.PartDuplicate:
		case slices.Contains(a.Overwrite, key):
			pp.Decision = appservice.DecideOverwrite
		case slices.Contains(a.Keep, key):
			pp.Decision = appservice.DecideKeep
		default:
			problems = append(problems, live.Problem{Code: "app_import_duplicate", Path: p.group + "." + p.typ + "[" + p.id + "]",
				Message: fmt.Sprintf("this tenant already has the %s %s (%s); the document has %s", p.typ, p.id, describe(*pp.Target), describe(pp.Document)),
				Fix:     fmt.Sprintf(`ask whether to overwrite it with the document's (overwrite: ["%s"]) or keep this tenant's (keep: ["%s"])`, key, key)})
		}
		plan.Parts = append(plan.Parts, pp)
	}
	for _, g := range []struct {
		typ string
		ids []string
	}{
		{apps.PartPrefab, idsOf(doc.AppParts.Prefabs, func(p apps.PrefabPart) string { return p.ID })},
		{apps.PartGraph, idsOf(doc.AppParts.Graphs, func(p apps.GraphPart) string { return p.ID })},
		{apps.PartEntity, idsOf(doc.AppParts.Entities, func(p apps.EntityPart) string { return p.ID })},
	} {
		for _, id := range g.ids {
			if _, ok := published(a.ServiceArgs, d, g.typ, id); !ok {
				problems = append(problems, live.Problem{Code: "app_requires", Path: "appParts." + g.typ + "[" + id + "]",
					Message: fmt.Sprintf("the app uses the %s %s of an app it requires, which this tenant does not have", g.typ, id),
					Fix:     "import and install the apps in requires.apps first"})
			}
		}
	}
	return plan, problems
}

func idsOf[T any](parts []T, id func(T) string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = id(p)
	}
	return out
}

// describe is one side of a part for a person deciding: version 3,
// published 2026-11-01 14:02 UTC, changed since.
func describe(v appservice.PartVersion) string {
	var parts []string
	if v.Version == 0 {
		parts = append(parts, "never published")
	} else {
		parts = append(parts, fmt.Sprintf("version %d", v.Version))
	}
	if !v.PublishedAt.IsZero() {
		parts = append(parts, "published "+v.PublishedAt.UTC().Format("2006-01-02 15:04 MST"))
	}
	if !v.UpdatedAt.IsZero() {
		parts = append(parts, "changed "+v.UpdatedAt.UTC().Format("2006-01-02 15:04 MST"))
	}
	if v.Changed {
		parts = append(parts, "with unpublished changes")
	}
	if v.Owner != "" {
		parts = append(parts, "owner "+v.Owner)
	}
	return strings.Join(parts, ", ")
}

// publishedAt is when a published version of a part was published, where
// its versions are kept.
func (s *service) publishedAt(args features.ServiceArgs, d Deps, typ, id string, version int) time.Time {
	if version == 0 || d.DB == nil {
		return time.Time{}
	}
	switch typ {
	case apps.PartPrefab:
		if _, row, err := prefabs.Version(args, prefabs.Deps{Projections: d.Projections, DB: d.DB}, id, version); err == nil {
			return row.PublishedAt
		}
	case apps.PartEntity:
		var row records.EntityTypeVersion
		if d.DB.Where("tenant_id = ? AND entity_id = ? AND version = ?", args.Principal.TenantID, id, version).First(&row).Error == nil {
			return row.PublishedAt
		}
	}
	return time.Time{}
}

// importer sends an import's commands.
type importer struct {
	args     features.ServiceArgs
	d        Deps
	doc      apps.Document
	plan     appservice.Plan
	run      uint64
	n        int
	last     uint64 // the sequence of the last command sent
	problems []live.Problem
}

// takes reports whether the import brings a part in: a new one, or one
// to overwrite.
func (im *importer) takes(typ, id string) bool {
	i := slices.IndexFunc(im.plan.Parts, func(p appservice.PartPlan) bool { return p.Type == typ && p.ID == id })
	return i >= 0 && (im.plan.Parts[i].Status == appservice.PartNew || im.plan.Parts[i].Decision == appservice.DecideOverwrite)
}

func (im *importer) overwrites(typ, id string) bool {
	i := slices.IndexFunc(im.plan.Parts, func(p appservice.PartPlan) bool { return p.Type == typ && p.ID == id })
	return i >= 0 && im.plan.Parts[i].Decision == appservice.DecideOverwrite
}

// run1 imports the document and returns the version published here.
func (im *importer) importAll() (int, error) {
	app := im.doc.App
	// The app's draft first, so its command and query graphs are never
	// live on their own once published
	if _, err := im.do("app.create", app, map[string]any{"name": im.doc.Manifest.Name}, manifests.DraftProjector, manifests.TargetType); err != nil {
		return 0, err
	}
	if err := im.entities(); err != nil {
		return 0, err
	}
	for _, p := range partsOf(im.doc) {
		if p.typ != apps.PartGraph || !im.takes(p.typ, p.id) {
			continue
		}
		if err := im.publish(p.typ, "bp.import", "bp.publish", p.id, map[string]any{"graph": p.def}, nil); err != nil {
			return 0, err
		}
	}
	for _, p := range im.prefabOrder() {
		allow := map[string]any{"allowBreaking": im.overwrites(apps.PartPrefab, p.ID)}
		if err := im.publish(apps.PartPrefab, "prefab.import", "prefab.publish", p.ID, map[string]any{"prefab": p}, allow); err != nil {
			return 0, err
		}
	}
	if _, err := im.do("app.setManifest", app, map[string]any{"manifest": im.doc.Manifest}, "", ""); err != nil {
		return 0, err
	}
	// connection fields are left empty for the tenant to fill (forge 07c)
	for _, e := range im.doc.Parts.Entities {
		for _, f := range e.Entity.Fields {
			if f.Type != "connection" {
				continue
			}
			for _, r := range im.doc.Seed[e.ID] {
				delete(r.Values, f.ID)
			}
		}
	}
	if len(im.doc.Seed) > 0 {
		if _, err := im.do("app.setSeed", app, map[string]any{"seed": im.doc.Seed}, "", ""); err != nil {
			return 0, err
		}
	}
	var dr manifests.Draft
	if err := im.read(manifests.DraftProjector, manifests.TargetType+"."+app, im.last, &dr); err != nil {
		return 0, err
	}
	res, err := im.send("app.publish", app, map[string]any{"rev": dr.Rev})
	if err != nil {
		return 0, err
	}
	// The draft's event comes first in the publish's batch, the version's last
	if err := im.read(manifests.DraftProjector, manifests.TargetType+"."+app, res.Seq-1, &dr); err != nil {
		return 0, err
	}
	if err := im.read(manifests.VersionProjector, manifests.VersionTargetType+"."+apps.VersionID(app, dr.Published), res.Seq, nil); err != nil {
		return 0, err
	}
	return dr.Published, nil
}

// entities imports the entity types the import takes. One that relates
// to an entity type not here yet is first imported without its relations
// and computed fields (and indexes and rules, which may name them), then
// whole once the others are; each is published once all are imported,
// the types it relates to first.
func (im *importer) entities() error {
	var take []apps.EntityPart
	for _, ps := range []apps.Parts{im.doc.Parts, im.doc.TenantParts} {
		for _, e := range ps.Entities {
			if im.takes(apps.PartEntity, e.ID) {
				take = append(take, e)
			}
		}
	}
	here := func(id string) bool {
		_, ok := part(im.args, im.d, apps.PartEntity, id)
		return ok || id == records.UserEntity
	}
	var later []apps.EntityPart
	for _, e := range take {
		def := e.Entity
		if slices.ContainsFunc(def.Fields, func(f records.Field) bool { return f.Relation != nil && !here(f.Relation.Entity) }) {
			def.Fields = slices.DeleteFunc(slices.Clone(def.Fields), func(f records.Field) bool { return f.Relation != nil || records.IsComputed(f) })
			def.Indexes, def.Access = nil, records.Access{}
			later = append(later, e)
		}
		if _, err := im.do("entity.import", e.ID, map[string]any{"entity": def}, "", apps.PartEntity); err != nil {
			return err
		}
	}
	for _, e := range later {
		if _, err := im.do("entity.import", e.ID, map[string]any{"entity": e.Entity}, "", apps.PartEntity); err != nil {
			return err
		}
	}
	done := map[string]bool{}
	var publish func(e apps.EntityPart) error
	publish = func(e apps.EntityPart) error {
		if done[e.ID] {
			return nil
		}
		done[e.ID] = true
		for _, f := range e.Entity.Fields {
			if f.Relation == nil {
				continue
			}
			if i := slices.IndexFunc(take, func(t apps.EntityPart) bool { return t.ID == f.Relation.Entity }); i >= 0 {
				if err := publish(take[i]); err != nil {
					return err
				}
			}
		}
		return im.publish(apps.PartEntity, "", "entity.publish", e.ID, nil, nil)
	}
	for _, e := range take {
		if err := publish(e); err != nil {
			return err
		}
	}
	return nil
}

// prefabOrder are the prefabs the import takes, each after those it uses.
func (im *importer) prefabOrder() []ui.Prefab {
	byID := map[string]ui.Prefab{}
	var ids []string
	for _, ps := range []apps.Parts{im.doc.Parts, im.doc.TenantParts} {
		for _, p := range ps.Prefabs {
			if im.takes(apps.PartPrefab, p.ID) {
				byID[p.ID] = p.Prefab
				ids = append(ids, p.ID)
			}
		}
	}
	var out []ui.Prefab
	done := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		p, ok := byID[id]
		if done[id] || !ok {
			return
		}
		done[id] = true
		for _, used := range ui.PrefabsUsed(p.Template) {
			visit(used)
		}
		out = append(out, p)
	}
	for _, id := range ids {
		visit(id)
	}
	return out
}

// publish imports a part (unless importCmd is empty) and publishes it at
// the rev the import left, unless it was already published as it is.
func (im *importer) publish(typ, importCmd, publishCmd, id string, importArgs, publishArgs map[string]any) error {
	k := kinds[typ]
	if importCmd != "" {
		if _, err := im.do(importCmd, id, importArgs, "", typ); err != nil {
			return err
		}
	}
	dr, ok := part(im.args, im.d, typ, id)
	if !ok {
		return log.Code("app_import_failed").Errorf("the %s %s was not imported", typ, id)
	}
	if dr.Published > 0 && dr.PublishedRev == dr.Rev {
		return nil
	}
	a := map[string]any{"rev": dr.Rev}
	for key, v := range publishArgs {
		a[key] = v
	}
	res, err := im.send(publishCmd, id, a)
	if err != nil {
		return err
	}
	// The next part's checks read this version
	return im.read(k.published, k.target+"."+id, res.Seq, nil)
}

// do sends a command and waits until its target's draft has folded it:
// projector and target type name the draft, or the part type does.
func (im *importer) do(typ, target string, a any, projector, targetType string) (bus.Result, error) {
	res, err := im.send(typ, target, a)
	if err != nil {
		return res, err
	}
	if k, ok := kinds[targetType]; ok {
		projector, targetType = k.draft, k.target
	}
	if projector == "" {
		projector, targetType = manifests.DraftProjector, manifests.TargetType
	}
	return res, im.read(projector, targetType+"."+target, res.Seq, nil)
}

// send sends one command as the principal.
func (im *importer) send(typ, target string, a any) (bus.Result, error) {
	im.n++
	raw, err := json.Marshal(a)
	if err != nil {
		return bus.Result{}, log.Wrap(err)
	}
	res := im.d.Bus.Execute(bus.Request{ServiceArgs: im.args, ID: fmt.Sprintf("import-%d-%d", im.run, im.n), Type: typ, Target: target, Args: raw})
	if res.Common.Code >= 300 {
		im.problems = res.Problems
		msg := res.Common.Message
		for _, p := range res.Problems {
			msg += fmt.Sprintf("; %s: %s", p.Path, p.Message)
		}
		return res, log.Code("app_import_refused").With("command", typ).With("target", target).Errorf("%s %s was refused: %s", typ, target, msg)
	}
	im.last = res.Seq
	return res, nil
}

// read reads a projected value once the commit at seq is folded into it.
func (im *importer) read(projector, key string, seq uint64, into any) error {
	v, err := im.d.Projections.WaitFor(im.args.Context(), im.args.Principal.TenantID, projector, key, seq, 5*time.Second)
	if err != nil {
		return log.Wrapf(err, "%s was not projected in time", key)
	}
	if into != nil && json.Unmarshal(v.Value, into) != nil {
		return log.Errorf("%s cannot be read", key)
	}
	return nil
}
