package prefabdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

type ListVersionsResponse struct {
	rest.Status
	Versions []VersionInfo `json:"versions"`
}

// ListVersionsResponseFrom converts the service's ListVersionsResponse.
func ListVersionsResponseFrom(resp prefabservice.ListVersionsResponse) ListVersionsResponse {
	return ListVersionsResponse{Status: rest.StatusFrom(resp.Common), Versions: versionsFrom(resp.Versions)}
}
