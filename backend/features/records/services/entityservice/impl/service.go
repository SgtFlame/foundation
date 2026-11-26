package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
	"gorm.io/gorm"
)

type service struct {
	deps entities.Deps
}

var log = records.Log.Tags("entityservice")

// check runs authz.Check for records.read on an entity type, or on the
// tenant's data model when id is empty, and makes sure the versions
// follower has caught up on what was published while the server was down.
func (s *service) check(args features.ServiceArgs, id string) *features.ServiceResponse {
	res := authz.Tenant(args.Principal) + "/records"
	if id != "" {
		res = records.Resource(args.Principal, id)
	}
	if err := authz.Check(args.Context(), args.Principal, "records.read", res).Err("records.read", res); err != nil {
		return args.Fail(authz.Status, err)
	}
	if err := entities.Follow(args, s.deps); err != nil {
		log.Error(args.Context(), err, "cannot follow the published entity types")
	}
	return nil
}

func (s *service) versions(args features.ServiceArgs) *gorm.DB {
	return s.deps.DB.Model(&records.EntityTypeVersion{}).Scopes(db.InTenant(args.Principal.TenantID))
}

func infoOf(v records.EntityTypeVersion) entityservice.VersionInfo {
	return entityservice.VersionInfo{EntityID: v.EntityID, Version: v.Version, Name: v.Name, Owner: v.Owner, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt}
}
