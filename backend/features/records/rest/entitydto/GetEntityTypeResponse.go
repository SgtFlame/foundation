package entitydto

import (
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// GetEntityTypeResponse carries the definition as its own JSON document
// (records.EntityType), the format it is published, exported and imported
// in.
type GetEntityTypeResponse struct {
	rest.Status
	Info       *VersionInfo        `json:"info,omitempty"`
	EntityType *records.EntityType `json:"entityType,omitempty"`
}

// GetEntityTypeResponseFrom converts the service's GetEntityTypeResponse.
func GetEntityTypeResponseFrom(resp entityservice.GetEntityTypeResponse) GetEntityTypeResponse {
	out := GetEntityTypeResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.EntityType.ID == "" {
		return out
	}
	info, e := VersionInfoFrom(resp.Info), resp.EntityType
	out.Info, out.EntityType = &info, &e
	return out
}
