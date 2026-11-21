package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

// Sections lists the registered backend sections with their schemas, then
// those the tenant's section sources give (installed apps').
func (s *service) Sections(args configservice.SectionsArgs) configservice.SectionsResponse {
	const action = "config.read"
	if err := authz.Check(args.Context(), args.Principal, action, "app").Err(action, "app"); err != nil {
		return configservice.SectionsResponse{Common: *args.Fail(authz.Status, err)}
	}
	all, _ := config.SectionsFor(args.Context(), s.registry, args.Principal.TenantID)
	return configservice.SectionsResponse{Common: features.OK(), Sections: all}
}
