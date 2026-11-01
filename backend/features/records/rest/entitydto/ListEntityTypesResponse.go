package entitydto

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListEntityTypesResponse struct {
	rest.Status
	EntityTypes []VersionInfo `json:"entityTypes"`
}

// ListEntityTypesResponseFrom converts the service's ListEntityTypesResponse.
func ListEntityTypesResponseFrom(resp entityservice.ListEntityTypesResponse) ListEntityTypesResponse {
	return ListEntityTypesResponse{Status: rest.StatusFrom(resp.Common), EntityTypes: versionsFrom(resp.EntityTypes)}
}
