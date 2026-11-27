package controlplane

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// AppID is the infrastructure library app (forge 20b-2, iteration question
// 12): the desired state's entity types, installed only in the root
// tenant. Forge's Infrastructure app requires it.
const AppID = "infrastructure"

// The desired state's entity types.
const (
	EntityHost       = "infraHost"
	EntityProfile    = "infraProfile"
	EntityInstance   = "infraInstance"
	EntityUnit       = "infraUnit"
	EntityPackagePin = "infraPackagePin"
	EntityEnrollment = "infraEnrollment"
	EntityNatsAcct   = "infraNatsAccount"
	EntityScript     = "infraScript"
)

//go:embed infra
var infraFiles embed.FS

// Document is the infrastructure app as an export document, which SeedApp
// imports into the root tenant.
func Document() (apps.Document, error) {
	d := apps.Document{Format: apps.DocumentFormat, App: AppID, Version: 1, ExportedAt: time.Now().UTC()}
	raw, err := infraFiles.ReadFile("infra/app.json")
	if err != nil {
		return d, log.Wrap(err)
	}
	if err := json.Unmarshal(raw, &d.Manifest); err != nil {
		return d, log.Wrapf(err, "infra/app.json")
	}
	names, err := fs.Glob(infraFiles, "infra/entities/*.json")
	if err != nil {
		return d, log.Wrap(err)
	}
	sort.Strings(names)
	for _, n := range names {
		raw, err := infraFiles.ReadFile(n)
		if err != nil {
			return d, log.Wrap(err)
		}
		var e records.EntityType
		if err := json.Unmarshal(raw, &e); err != nil {
			return d, log.Wrapf(err, "%s", n)
		}
		e.Version = 1
		d.Parts.Entities = append(d.Parts.Entities, apps.EntityPart{ID: e.ID, Version: 1, Entity: e})
	}
	return d, nil
}

// RootArgs are the root tenant's service principal's arguments, which
// seeding and the followers act as.
func RootArgs(args features.ServiceArgs) features.ServiceArgs {
	return features.ServiceArgs{Ctx: args.Ctx, Principal: features.ServicePrincipal(features.RootTenantID), Meta: args.Meta}
}

// SeedApp imports the infrastructure app into the root tenant and installs
// it, once: an installed app is left alone, and an imported one is only
// installed.
func SeedApp(args features.ServiceArgs, store live.Store, svc appservice.Service, b *bus.Bus) error {
	args = RootArgs(args)
	ctx := args.Context()
	if p, ok := store.(interface {
		Settle(context.Context, uint64, string) error
	}); ok {
		_ = p.Settle(ctx, features.RootTenantID, installs.Projector)
	}
	if _, ok := installs.Get(ctx, store, features.RootTenantID, AppID); ok {
		return nil
	}
	if _, ok := manifests.GetVersion(args, store, AppID, 1); !ok {
		d, err := Document()
		if err != nil {
			return err
		}
		resp := svc.Import(appservice.ImportArgs{ServiceArgs: args, Document: d})
		if resp.Common.Code >= 300 && resp.Common.ErrorCode != "app_import_exists" {
			return log.Code("seed_failed").Errorf("cannot import the infrastructure app: %s", problems(resp.Common.Message, resp))
		}
	}
	raw, _ := json.Marshal(map[string]any{"version": 1})
	res := b.Execute(bus.Request{ServiceArgs: args, ID: "controlplane-install-" + AppID, Type: "app.install", Target: AppID, Args: raw})
	if res.Common.Code >= 300 {
		return log.Code("seed_failed").Errorf("cannot install the infrastructure app: %s", res.Common.Message)
	}
	log.Info(ctx, "installed the infrastructure app in the root tenant")
	return nil
}

func problems(msg string, v any) string {
	raw, _ := json.Marshal(v)
	s := string(raw)
	if len(s) > 600 {
		s = s[:600]
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", msg, s))
}

// ScriptKeyPath is where the root tenant's script signing key is kept
// (forge 20c-3): with the deployment's NATS keys, else in the data
// directory.
func ScriptKeyPath(keysDir, dataDir string) string {
	if keysDir != "" {
		return filepath.Join(keysDir, "script-signing.key")
	}
	return filepath.Join(dataDir, "script-signing.key")
}
