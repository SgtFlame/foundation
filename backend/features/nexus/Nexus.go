// Package nexus is the Nexus library app (forge 26d-3, iteration 26
// question 9): native channels' entity types, roles and row rules, command
// and query graphs, the unread projector, the triggers that keep private
// channels' grants and notify mentions, and the tools agents use, kept as
// the files under app/ (laid out as a Forge sample: app.json, entities/,
// graphs/). Seed imports and installs it in a tenant; Forge's starter
// seeds it with the other library apps (Files), and Forge's Messaging app
// requires it.
package nexus

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/records"
)

var log = logging.In("nexus")

// AppID is the library app's id.
const AppID = "nexus"

// The entity types.
const (
	EntityChannel  = "nexusChannel"
	EntityMember   = "nexusMember"
	EntityMessage  = "nexusMessage"
	EntityReaction = "nexusReaction"
)

//go:embed app
var files embed.FS

// Files are the app's files (app.json, entities/, graphs/), as Forge's
// starter loads a library app.
func Files() fs.FS {
	sub, _ := fs.Sub(files, "app")
	return sub
}

// Document is the app as an export document, which Seed imports.
func Document() (apps.Document, error) {
	d := apps.Document{Format: apps.DocumentFormat, App: AppID, Version: 1, ExportedAt: time.Now().UTC()}
	fsys := Files()
	raw, err := fs.ReadFile(fsys, "app.json")
	if err != nil {
		return d, log.Wrap(err)
	}
	if err := json.Unmarshal(raw, &d.Manifest); err != nil {
		return d, log.Wrapf(err, "app.json")
	}
	names, _ := fs.Glob(fsys, "entities/*.json")
	sort.Strings(names)
	for _, n := range names {
		raw, err := fs.ReadFile(fsys, n)
		if err != nil {
			return d, log.Wrap(err)
		}
		var e records.EntityType
		if err := json.Unmarshal(raw, &e); err != nil {
			return d, log.Wrapf(err, "%s", n)
		}
		e.ID = strings.TrimSuffix(strings.TrimPrefix(n, "entities/"), ".json")
		e.Version = 1
		d.Parts.Entities = append(d.Parts.Entities, apps.EntityPart{ID: e.ID, Version: 1, Entity: e})
	}
	nodes := map[string]bool{}
	names, _ = fs.Glob(fsys, "graphs/*.json")
	sort.Strings(names)
	for _, n := range names {
		raw, err := fs.ReadFile(fsys, n)
		if err != nil {
			return d, log.Wrap(err)
		}
		var g cognition.Graph
		if err := json.Unmarshal(raw, &g); err != nil {
			return d, log.Wrapf(err, "%s", n)
		}
		g.Version = 1
		d.Parts.Graphs = append(d.Parts.Graphs, apps.GraphPart{ID: g.ID, Version: 1, Graph: g})
		for _, n := range g.Nodes {
			nodes[n.Type] = true
		}
	}
	d.Primitives.Components = []string{}
	d.Primitives.Graphs = []string{}
	d.Primitives.Nodes = slices.Sorted(maps.Keys(nodes))
	return d, nil
}

// Seed imports the app into args' tenant and installs it, once, as the
// tenant's service principal: an installed app is left alone, and an
// imported one is only installed.
func Seed(args features.ServiceArgs, store live.Store, svc appservice.Service, b *bus.Bus) error {
	args = features.ServiceArgs{Ctx: args.Ctx, Principal: features.ServicePrincipal(args.Principal.TenantID), Meta: args.Meta}
	tenant := args.Principal.TenantID
	ctx := args.Context()
	if p, ok := store.(interface {
		Settle(context.Context, uint64, string) error
	}); ok {
		_ = p.Settle(ctx, tenant, installs.Projector)
	}
	if _, ok := installs.Get(ctx, store, tenant, AppID); ok {
		return nil
	}
	if _, ok := manifests.GetVersion(args, store, AppID, 1); !ok {
		d, err := Document()
		if err != nil {
			return err
		}
		resp := svc.Import(appservice.ImportArgs{ServiceArgs: args, Document: d})
		if resp.Common.Code >= 300 && resp.Common.ErrorCode != "app_import_exists" {
			raw, _ := json.Marshal(resp)
			s := string(raw)
			if len(s) > 1500 {
				s = s[:1500]
			}
			return log.Code("seed_failed").Errorf("cannot import the nexus app: %s %s", resp.Common.Message, s)
		}
	}
	raw, _ := json.Marshal(map[string]any{"version": 1})
	res := b.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("nexus-install-%d", tenant), Type: "app.install", Target: AppID, Args: raw})
	if res.Common.Code >= 300 {
		return log.Code("seed_failed").Errorf("cannot install the nexus app: %s", res.Common.Message)
	}
	return nil
}
