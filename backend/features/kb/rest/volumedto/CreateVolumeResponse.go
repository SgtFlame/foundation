package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type CreateVolumeResponse struct {
	rest.Status
	VolumeID uint64 `json:"volumeId,string,omitempty"`
}

// CreateVolumeResponseFrom converts the service's CreateVolumeResponse; its
// code is carried by the HTTP status rather than the body.
func CreateVolumeResponseFrom(resp volumeservice.CreateVolumeResponse) CreateVolumeResponse {
	return CreateVolumeResponse{
		Status:   rest.StatusFrom(resp.Common),
		VolumeID: resp.VolumeID,
	}
}
