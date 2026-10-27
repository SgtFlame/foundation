package kbdto

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type OpenVolumeResponse struct {
	rest.Status
	VolumeID uint64 `json:"volumeId,string,omitempty"`
}

// OpenVolumeResponseFrom converts the service's OpenVolumeResponse; its code is
// carried by the HTTP status rather than the body.
func OpenVolumeResponseFrom(resp kbservice.OpenVolumeResponse) OpenVolumeResponse {
	return OpenVolumeResponse{
		Status:   rest.StatusFrom(resp.Common),
		VolumeID: resp.VolumeID,
	}
}
