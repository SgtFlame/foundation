package manifests

import (
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Follower is the durable consumer that copies apps into the tenant
// database.
const Follower = "apps.apps"

// Follow makes sure the principal's tenant copies its apps into the
// tenant database (apps_apps): app.create starts it before its event
// commits, and app.list after a restart, catching up on what changed
// meanwhile.
func Follow(args features.ServiceArgs, deps Deps) error {
	if deps.DB == nil {
		return nil
	}
	tenant := args.Principal.TenantID
	t := live.Target{Area: live.AreaDefinitions, Type: TargetType, ID: "*"}
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, Follower,
		[]string{t.Subject(tenant, EventCreated), t.Subject(tenant, EventInfoSet)}, func(e live.Event) error {
			return copyApp(deps, e)
		})
}

// copyApp keeps an app's row in step with its created and infoSet events;
// a redelivered event writes the same values again.
func copyApp(deps Deps, e live.Event) error {
	row := apps.AppRow{Tenanted: db.Tenanted{TenantID: e.TenantID}, AppID: e.Target.ID}
	if err := deps.DB.Where(row).Attrs(apps.AppRow{CreatedBy: e.UserID}).FirstOrCreate(&row).Error; err != nil {
		return err
	}
	set := map[string]any{}
	switch e.Type {
	case EventCreated:
		v, err := decode[struct{ Manifest apps.Manifest }](e.Data)
		if err != nil {
			return fmt.Errorf("cannot read the app: %w", err)
		}
		set["name"], set["icon"], set["description"] = v.Manifest.Name, v.Manifest.Icon, v.Manifest.Description
	case EventInfoSet:
		in, err := decode[Info](e.Data)
		if err != nil {
			return fmt.Errorf("cannot read the app's info: %w", err)
		}
		for col, v := range map[string]*string{"name": in.Name, "icon": in.Icon, "description": in.Description} {
			if v != nil {
				set[col] = *v
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return deps.DB.Model(&row).Updates(set).Error
}

// VersionsFollower is the durable consumer that copies published versions
// into the tenant database.
const VersionsFollower = "apps.versions"

// FollowVersions makes sure the principal's tenant copies its published
// app versions into the tenant database (apps_versions): app.publish
// starts it before its event commits, and app.versions after a restart.
func FollowVersions(args features.ServiceArgs, deps Deps) error {
	if deps.DB == nil {
		return nil
	}
	tenant := args.Principal.TenantID
	subject := live.Target{Area: live.AreaDefinitions, Type: VersionTargetType, ID: "*"}.Subject(tenant, EventPublished)
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, VersionsFollower, []string{subject}, func(e live.Event) error {
		v, err := decode[apps.Version](e.Data)
		if err != nil {
			return fmt.Errorf("cannot read the version: %w", err)
		}
		row := apps.VersionRow{Tenanted: db.Tenanted{TenantID: e.TenantID}, AppID: v.App, Version: v.Version}
		return deps.DB.Where(row).Attrs(apps.VersionRow{Rev: v.Rev, Definition: string(e.Data),
			PublishedBy: e.UserID, PublishedAt: e.Time, Stream: e.Stream, Seq: e.Seq}).FirstOrCreate(&row).Error
	})
}
