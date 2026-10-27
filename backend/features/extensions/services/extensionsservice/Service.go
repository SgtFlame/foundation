package extensionsservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Service describes the backend's extension points and what contributes to
// them, for DevTools' Extensions tab.
type Service interface {
	ListPoints(args ListPointsArgs) ListPointsResponse
}

type ListPointsArgs struct {
	features.ServiceArgs
}

type ListPointsResponse struct {
	Common features.ServiceResponse
	Points []extensions.PointInfo // in declaration order
}
