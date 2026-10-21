package volumedto

import "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"

type CreateVolumeResponse struct {
	Message  string `json:"message,omitempty"`
	VolumeID uint64 `json:"volumeId,string,omitempty"`
}

// CreateVolumeResponseFrom converts the service's CreateVolumeResponse; its
// code is carried by the HTTP status rather than the body.
func CreateVolumeResponseFrom(resp volumeservice.CreateVolumeResponse) CreateVolumeResponse {
	return CreateVolumeResponse{
		Message:  resp.Common.Message,
		VolumeID: resp.VolumeID,
	}
}
