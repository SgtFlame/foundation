// Package installs installs published apps into a tenant (forge 05b,
// documentation/Apps.md › Built in slice 32): app.install, app.upgrade and
// app.uninstall are commands on the app's install, an aggregate on the def
// stream folded into the apps.install projection, which holds the version
// it runs whole. An installed app's commands and queries are on the live
// core as <appId>.<name>, decided and answered by the graphs its version
// pinned (CommandFor, QueryFor); app.installs lists what is installed.
package installs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"gorm.io/gorm"
)

var log = apps.Log.Tags("installs")

// TargetType is an app's install on the def stream; its id is the app's.
const TargetType = "appInstall"

// Projector holds each app's install, keyed appInstall.<appId>.
const Projector = "apps.install"

// Follower copies installs into the tenant database.
const Follower = "apps.installs"

// Events an install's commands commit.
const (
	EventInstalled   = "installed"   // {version}: apps.Version
	EventUpgraded    = "upgraded"    // {from, version}
	EventUninstalled = "uninstalled" // {}
)

// Projections are what installs read: the projections, waiting for a
// commit to be projected, and following the def stream.
type Projections interface {
	live.Store
	WaitFor(ctx context.Context, tenant uint64, projector, key string, seq uint64, timeout time.Duration) (live.Projected, error)
	Follow(ctx context.Context, tenant uint64, area live.Area, name string, subjects []string, fn func(live.Event) error) error
}

// Deps are what the commands, queries and resolvers need, made after the
// registry.
type Deps struct {
	Projections Projections
	DB          *gorm.DB       // the tenant database, which holds the installs
	Nodes       livenodes.Deps // runs the pinned graphs
	// Roles makes and removes the apps' roles (forge 05c); nil makes none.
	Roles *authz.Policy
	// Config resolves the apps' settings sections and tells their changes.
	Config configservice.Service
}

type change struct {
	From    int           `json:"from,omitempty"`
	Version *apps.Version `json:"version,omitempty"`
}

func problem(path, message, fix string) []live.Problem {
	return []live.Problem{{Path: path, Message: message, Fix: fix}}
}

// Get reads an app's install in the principal's tenant.
func Get(ctx context.Context, store live.Store, tenant uint64, app string) (apps.Install, bool) {
	var in apps.Install
	v, ok, err := store.Get(ctx, tenant, Projector, TargetType+"."+app)
	if err != nil || !ok || v.Value == nil || json.Unmarshal(v.Value, &in) != nil {
		return apps.Install{}, false
	}
	return in, true
}

// Running is an app's install when it is installed.
func Running(ctx context.Context, store live.Store, tenant uint64, app string) (apps.Install, bool) {
	in, ok := Get(ctx, store, tenant, app)
	return in, ok && in.Installed()
}

type decision struct {
	live.Decision
	deps    Deps
	reg     *extensions.Registry
	install apps.Install
	exists  bool
}

// version reads the version asked for (the last when 0), checking that the
// tenant has what it requires.
func (c *decision) version(n int) (apps.Version, []live.Problem) {
	v, ok := manifests.GetVersion(c.ServiceArgs, c.deps.Projections, c.Target.ID, n)
	if !ok {
		if n == 0 {
			return v, problem("target", fmt.Sprintf("the app %s has no published version", c.Target.ID), "publish it first with app.publish")
		}
		return v, problem("args.version", fmt.Sprintf("the app %s has no version %d", c.Target.ID, n), "list its versions with app.versions")
	}
	missing := apps.Missing(v.Manifest.Requires, ui.Catalog(c.reg), func(app string) bool {
		_, ok := Running(c.Context(), c.deps.Projections, c.Principal.TenantID, app)
		return ok
	})
	return v, missing
}

type spec struct {
	id, title, description, args, example string
	events                                []string
	decide                                func(c *decision, version int) ([]live.NewEvent, []live.Problem)
}

const versionArgs = `{"type":"object","properties":{"version":{"type":"integer","minimum":1,"description":"A published version; the last when left out"}}}`

func specs() []spec {
	return []spec{
		{
			id: "app.install", title: "Install an app", events: []string{EventInstalled}, args: versionArgs, example: `{}`,
			description: "Installs a published app into this tenant (the target is the app's id): its commands and queries go live as <appId>.<name>, decided and answered by the graphs its version pinned, and its triggers are recorded (they run from forge iteration 11). It fails, naming what is missing, when the tenant lacks a primitive or an app the app requires. An app is installed once; later versions come by app.upgrade.",
			decide: func(c *decision, n int) ([]live.NewEvent, []live.Problem) {
				if c.exists && c.install.Installed() {
					return nil, problem("target", fmt.Sprintf("the app %s is installed already, at version %d", c.Target.ID, c.install.Version.Version), "take a newer version with app.upgrade")
				}
				v, problems := c.version(n)
				if problems != nil {
					return nil, problems
				}
				return []live.NewEvent{{Type: EventInstalled, Data: change{Version: &v}}}, nil
			},
		},
		{
			id: "app.upgrade", title: "Upgrade an app", events: []string{EventUpgraded}, args: versionArgs, example: `{}`,
			description: "Takes an installed app to a newer published version, in place: entity changes apply without migrations, commands the new version removed are refused from then on (one sent before and decided after is decided by the new graph), and every subscription to its queries takes a fresh snapshot under the new version.",
			decide: func(c *decision, n int) ([]live.NewEvent, []live.Problem) {
				if !c.exists || !c.install.Installed() {
					return nil, problem("target", fmt.Sprintf("the app %s is not installed", c.Target.ID), "install it with app.install")
				}
				v, problems := c.version(n)
				if problems != nil {
					return nil, problems
				}
				if cur := c.install.Version.Version; v.Version <= cur {
					return nil, problem("args.version", fmt.Sprintf("the app %s is at version %d already, and %d is not newer", c.Target.ID, cur, v.Version), "publish a new version with app.publish, then upgrade")
				}
				return []live.NewEvent{{Type: EventUpgraded, Data: change{From: c.install.Version.Version, Version: &v}}}, nil
			},
		},
		{
			id: "app.uninstall", title: "Uninstall an app", events: []string{EventUninstalled}, args: `{"type":"object","properties":{}}`, example: `{}`,
			description: "Uninstalls an app from this tenant: its commands and queries leave the live core and its triggers stop; its records are kept until a purge, and installing it again finds them.",
			decide: func(c *decision, _ int) ([]live.NewEvent, []live.Problem) {
				if !c.exists || !c.install.Installed() {
					return nil, problem("target", fmt.Sprintf("the app %s is not installed", c.Target.ID), "")
				}
				return []live.NewEvent{{Type: EventUninstalled, Data: map[string]any{}}}, nil
			},
		},
	}
}

// Contribute registers the install projector, commands and query; deps
// returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: "apps"}, Title: "App installs",
		Description: "Each app's install in the tenant: {app, status, version (the apps.Version it runs, whole), installedBy, installedAt, updatedBy, updatedAt}",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: fold,
	})
	for _, s := range specs() {
		live.Commands.Register(reg, live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "apps"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events,
			Area: live.AreaDefinitions, TargetType: TargetType, Projection: Projector, Action: "app.install",
			Resource: func(p features.Principal, t live.Target) string { return apps.Resource(p, t.ID) },
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				c := &decision{Decision: d, deps: deps(), reg: reg}
				if d.State != nil {
					if err := json.Unmarshal(d.State, &c.install); err != nil {
						return nil, problem("", "the install cannot be read: "+err.Error(), "")
					}
					c.exists = true
				}
				var a struct{ Version int }
				_ = json.Unmarshal(d.Args, &a)
				events, problems := s.decide(c, a.Version)
				if problems != nil {
					return nil, problems
				}
				// The copy into the tenant database runs before the event lands
				if err := Follow(c.ServiceArgs, c.deps); err != nil {
					return nil, problem("", "the install cannot be kept: "+err.Error(), "")
				}
				return events, nil
			},
			Report: func(args features.ServiceArgs, t live.Target, seq uint64) any {
				v, err := deps().Projections.WaitFor(args.Context(), args.Principal.TenantID, Projector, t.Key(), seq, 2*time.Second)
				var in apps.Install
				if err != nil || json.Unmarshal(v.Value, &in) != nil {
					return nil
				}
				return summaryOf(in)
			},
		})
	}
	contributeQueries(reg, deps)
	contributeSettings(reg, deps)
}

// fold is the apps.install projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	var in apps.Install
	if f.State != nil {
		if err := json.Unmarshal(f.State, &in); err != nil {
			return nil, err
		}
	}
	e := f.Event
	var c change
	if err := json.Unmarshal(e.Data, &c); err != nil {
		return nil, err
	}
	switch e.Type {
	case EventInstalled:
		in = apps.Install{App: e.Target.ID, Status: apps.InstallInstalled, InstalledBy: e.UserID, InstalledAt: e.Time}
		in.Version = *c.Version
	case EventUpgraded:
		in.Version = *c.Version
	case EventUninstalled:
		in.Status = apps.InstallUninstalled
	default:
		return f.State, nil
	}
	in.UpdatedBy, in.UpdatedAt = e.UserID, e.Time
	return json.Marshal(in)
}

// Follow makes sure the principal's tenant copies its installs into the
// tenant database (apps_installs) and keeps its apps' roles (forge 05c):
// the install commands start it before their events commit, and
// app.installs after a restart.
func Follow(args features.ServiceArgs, deps Deps) error {
	if err := followAccess(args, deps); err != nil {
		return err
	}
	if deps.DB == nil {
		return nil
	}
	tenant := args.Principal.TenantID
	t := live.Target{Area: live.AreaDefinitions, Type: TargetType, ID: "*"}
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, Follower, []string{t.Subjects(tenant)}, func(e live.Event) error {
		var c change
		if err := json.Unmarshal(e.Data, &c); err != nil {
			return fmt.Errorf("cannot read the install: %w", err)
		}
		row := apps.InstallRow{Tenanted: db.Tenanted{TenantID: e.TenantID}, AppID: e.Target.ID}
		if err := deps.DB.Where(row).FirstOrCreate(&row).Error; err != nil {
			return err
		}
		set := map[string]any{"changed_by": e.UserID, "changed_at": e.Time}
		switch e.Type {
		case EventInstalled:
			set["status"], set["version"], set["installed_by"], set["installed_at"] = apps.InstallInstalled, c.Version.Version, e.UserID, e.Time
		case EventUpgraded:
			set["version"] = c.Version.Version
		case EventUninstalled:
			set["status"] = apps.InstallUninstalled
		}
		return deps.DB.Model(&row).Updates(set).Error
	})
}
