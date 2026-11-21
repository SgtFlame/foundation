package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
)

// Components lists the primitive components with their metadata.
func (s *service) Components(args uiservice.ComponentsArgs) uiservice.ComponentsResponse {
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, "ui.read", resource).Err("ui.read", resource); err != nil {
		return uiservice.ComponentsResponse{Common: *args.Fail(authz.Status, err)}
	}
	return uiservice.ComponentsResponse{Common: features.OK(), Components: ui.Components.All(s.reg)}
}
