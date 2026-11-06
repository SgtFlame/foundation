package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
	"gorm.io/gorm"
)

type service struct {
	deps prefabs.Deps
	reg  *extensions.Registry
	subs *subscribe.Subscriptions
}

var log = ui.Log.Tags("prefabservice")

// check runs authz.Check for prefabs.read on a prefab, or on the tenant's
// prefabs when id is empty, and makes sure the versions follower has
// caught up on what was published while the server was down.
func (s *service) check(args features.ServiceArgs, id string) *features.ServiceResponse {
	res := ui.PrefabResource(args.Principal, id)
	if err := authz.Check(args.Context(), args.Principal, "prefabs.read", res).Err("prefabs.read", res); err != nil {
		return args.Fail(authz.Status, err)
	}
	if err := prefabs.Follow(args, s.deps); err != nil {
		log.Error(args.Context(), err, "cannot follow the published prefabs")
	}
	return nil
}

func (s *service) versions(args features.ServiceArgs) *gorm.DB {
	return s.deps.DB.Model(&ui.PrefabVersion{}).Scopes(db.InTenant(args.Principal.TenantID))
}

func infoOf(v ui.PrefabVersion) prefabservice.VersionInfo {
	return prefabservice.VersionInfo{PrefabID: v.PrefabID, Version: v.Version, Name: v.Name, Owner: v.Owner, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt}
}
