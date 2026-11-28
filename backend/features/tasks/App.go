// Package tasks is the Tasks library app (forge 30c): the tenant's to-dos
// in one place, which Mail, the Nexus, the CRM and Forge's Tasks app
// require. Its entity types, command, query, trigger and tool graphs,
// roles and row rules are embedded here and seeded into a tenant as
// controlplane.SeedApp seeds the infrastructure app.
package tasks

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
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

var log = logging.In("tasks")

// AppID is the Tasks library app.
const AppID = "tasks"

// Its entity types.
const (
	EntityProject = "tasksProject"
	EntityStatus  = "tasksStatus"
	EntityTask    = "taskItem"
	EntityCheck   = "taskCheck"
	EntityComment = "taskComment"
	EntityLabel   = "taskLabel"
)

//go:embed app
var appFiles embed.FS

// Document is the Tasks app as an export document, which SeedApp imports.
func Document() (apps.Document, error) {
	d := apps.Document{Format: apps.DocumentFormat, App: AppID, Version: 1, ExportedAt: time.Now().UTC()}
	raw, err := appFiles.ReadFile("app/app.json")
	if err != nil {
		return d, log.Wrap(err)
	}
	if err := json.Unmarshal(raw, &d.Manifest); err != nil {
		return d, log.Wrapf(err, "app/app.json")
	}
	if err := each("app/entities/*.json", func(name string, raw []byte) error {
		var e records.EntityType
		if err := json.Unmarshal(raw, &e); err != nil {
			return log.Wrapf(err, "%s", name)
		}
		e.Version = 1
		d.Parts.Entities = append(d.Parts.Entities, apps.EntityPart{ID: e.ID, Version: 1, Entity: e})
		return nil
	}); err != nil {
		return d, err
	}
	nodes := map[string]bool{}
	if err := each("app/graphs/*.json", func(name string, raw []byte) error {
		var g cognition.Graph
		if err := json.Unmarshal(raw, &g); err != nil {
			return log.Wrapf(err, "%s", name)
		}
		for _, n := range g.Nodes {
			nodes[n.Type] = true
		}
		d.Parts.Graphs = append(d.Parts.Graphs, apps.GraphPart{ID: g.ID, Version: 1, Graph: g})
		return nil
	}); err != nil {
		return d, err
	}
	for n := range nodes {
		d.Primitives.Nodes = append(d.Primitives.Nodes, n)
	}
	slices.Sort(d.Primitives.Nodes)
	return d, nil
}

func each(pattern string, fn func(name string, raw []byte) error) error {
	names, err := fs.Glob(appFiles, pattern)
	if err != nil {
		return log.Wrap(err)
	}
	sort.Strings(names)
	for _, n := range names {
		raw, err := appFiles.ReadFile(n)
		if err != nil {
			return log.Wrap(err)
		}
		if err := fn(n, raw); err != nil {
			return err
		}
	}
	return nil
}

// SeedApp imports the Tasks app into a tenant and installs it, once, as
// the tenant's service principal: an installed app is left alone, and an
// imported one is only installed. Its installer role (manager) goes to
// that principal; people get theirs by grants (Tasks user to make
// projects), and their projects' roles from the app.
func SeedApp(args features.ServiceArgs, tenant uint64, store live.Store, svc appservice.Service, b *bus.Bus) error {
	args = features.ServiceArgs{Ctx: args.Ctx, Principal: features.ServicePrincipal(tenant), Meta: args.Meta}
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
			return log.Code("seed_failed").Errorf("cannot import the Tasks app: %s", problems(resp.Common.Message, resp))
		}
	}
	raw, _ := json.Marshal(map[string]any{"version": 1})
	res := b.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("tasks-install-%d", tenant), Type: "app.install", Target: AppID, Args: raw})
	if res.Common.Code >= 300 {
		return log.Code("seed_failed").Errorf("cannot install the Tasks app: %s", res.Common.Message)
	}
	log.Info(ctx, "installed the Tasks app in tenant %d", tenant)
	return nil
}

func problems(msg string, v any) string {
	raw, _ := json.Marshal(v)
	s := string(raw)
	if len(s) > 800 {
		s = s[:800]
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", msg, s))
}
