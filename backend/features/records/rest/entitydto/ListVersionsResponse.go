package entitydto

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListVersionsResponse struct {
	rest.Status
	Versions []VersionInfo `json:"versions"`
}

// ListVersionsResponseFrom converts the service's ListVersionsResponse.
func ListVersionsResponseFrom(resp entityservice.ListVersionsResponse) ListVersionsResponse {
	return ListVersionsResponse{Status: rest.StatusFrom(resp.Common), Versions: versionsFrom(resp.Versions)}
}
