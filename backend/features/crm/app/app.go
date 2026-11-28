// Package crmapp is the CRM library app (forge 40d, iteration question
// 40.1): its entity types (companies, contacts, pipelines and stages,
// deals, activities and saved views), its command, query and tool graphs
// and its manifest with the Sales rep and Sales manager roles, kept as JSON
// under files/ laid out as a sample app is (app.json, entities/, graphs/).
// Forge seeds it into every tenant beside its own library apps, and its CRM
// app (40e) requires it.
package crmapp

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// AppID is the CRM library app's id.
const AppID = "crm"

// The app's entity types.
const (
	EntityCompany   = "crmCompany"
	EntityContact   = "crmContact"
	EntityPipeline  = "crmPipeline"
	EntityStage     = "crmStage"
	EntityDeal      = "crmDeal"
	EntityActivity  = "crmActivity"
	EntitySavedView = "crmSavedView"
)

var log = logging.In("crm")

//go:embed files
var embedded embed.FS

// Files are the app's definitions: app.json, entities/*.json and
// graphs/*.json, as Forge's starter.LoadDir reads a sample app.
var Files fs.FS = mustSub(embedded, "files")

func mustSub(f fs.FS, dir string) fs.FS {
	s, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return s
}

// Document is the app as an export document (version 1), for importing it
// with appservice as controlplane.SeedApp does.
func Document() (apps.Document, error) {
	d := apps.Document{Format: apps.DocumentFormat, App: AppID, Version: 1, ExportedAt: time.Now().UTC()}
	raw, err := fs.ReadFile(Files, "app.json")
	if err != nil {
		return d, log.Wrap(err)
	}
	if err := json.Unmarshal(raw, &d.Manifest); err != nil {
		return d, log.Wrapf(err, "app.json")
	}
	for _, n := range names("entities") {
		var e records.EntityType
		if err := read(path.Join("entities", n), &e); err != nil {
			return d, err
		}
		e.ID, e.Version = strings.TrimSuffix(n, ".json"), 1
		d.Parts.Entities = append(d.Parts.Entities, apps.EntityPart{ID: e.ID, Version: 1, Entity: e})
	}
	for _, n := range names("graphs") {
		var g cognition.Graph
		if err := read(path.Join("graphs", n), &g); err != nil {
			return d, err
		}
		d.Parts.Graphs = append(d.Parts.Graphs, apps.GraphPart{ID: g.ID, Version: 1, Graph: g})
	}
	return d, nil
}

func names(dir string) []string {
	entries, _ := fs.ReadDir(Files, dir)
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func read(name string, v any) error {
	raw, err := fs.ReadFile(Files, name)
	if err != nil {
		return log.Wrap(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return log.Wrapf(err, "%s", name)
	}
	return nil
}
