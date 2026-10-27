package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions/services/extensionsservice"
)

// ListPoints describes every declared point with its contributions.
func (s *service) ListPoints(args extensionsservice.ListPointsArgs) extensionsservice.ListPointsResponse {
	const action = "extensions.read"
	if err := authz.Check(args.Context(), args.Principal, action, "app").Err(action, "app"); err != nil {
		return extensionsservice.ListPointsResponse{Common: *args.Fail(authz.Status, err)}
	}
	return extensionsservice.ListPointsResponse{Common: features.OK(), Points: s.registry.Points()}
}
