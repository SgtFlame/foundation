package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

// Sections lists the registered backend sections with their schemas.
func (s *service) Sections(args configservice.SectionsArgs) configservice.SectionsResponse {
	const action = "config.read"
	if err := authz.Check(args.Context(), args.Principal, action, "app").Err(action, "app"); err != nil {
		return configservice.SectionsResponse{Common: *args.Fail(authz.Status, err)}
	}
	return configservice.SectionsResponse{Common: features.OK(), Sections: config.Sections.All(s.registry)}
}
