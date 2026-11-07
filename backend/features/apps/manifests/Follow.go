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
