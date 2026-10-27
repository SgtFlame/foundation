package extensionsdto

import (
	"github.com/SgtFlame/foundation/backend/features/extensions/services/extensionsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListPointsResponse struct {
	rest.Status
	Points []PointInfo `json:"points,omitempty"`
}

// ListPointsResponseFrom converts the service's ListPointsResponse; its code
// is carried by the HTTP status rather than the body.
func ListPointsResponseFrom(resp extensionsservice.ListPointsResponse) ListPointsResponse {
	out := ListPointsResponse{Status: rest.StatusFrom(resp.Common)}
	for _, p := range resp.Points {
		out.Points = append(out.Points, PointInfoFrom(p))
	}
	return out
}
