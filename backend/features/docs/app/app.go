// Package docsapp is the docs library app (forge 35a-2): its entity types
// (pages and favourites), its command, query, tool and trigger graphs and
// its manifest, kept as JSON under files/ laid out as a sample app is
// (app.json, entities/, graphs/). Forge seeds it into every tenant beside
// its own library apps, and its Docs app (35a-4) requires it. The nodes
// its graphs reach Volumes with are features/docs/nodes.
package docsapp

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/docs"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// AppID is the docs library app's id.
const AppID = docs.AppID

// The app's entity types.
const (
	EntityPage      = docs.EntityPage
	EntityFavourite = "docsFavourite"
)

var log = logging.In("docs")

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
		for _, n := range g.Nodes {
			if !slices.Contains(d.Primitives.Nodes, n.Type) {
				d.Primitives.Nodes = append(d.Primitives.Nodes, n.Type)
			}
		}
	}
	sort.Strings(d.Primitives.Nodes) // the node types its graphs use, so appservice imports it
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
