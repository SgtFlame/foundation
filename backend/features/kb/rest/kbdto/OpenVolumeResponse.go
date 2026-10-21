package kbdto

import "github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"

type OpenVolumeResponse struct {
	Message  string `json:"message,omitempty"`
	VolumeID uint64 `json:"volumeId,string,omitempty"`
}

// OpenVolumeResponseFrom converts the service's OpenVolumeResponse; its code is
// carried by the HTTP status rather than the body.
func OpenVolumeResponseFrom(resp kbservice.OpenVolumeResponse) OpenVolumeResponse {
	return OpenVolumeResponse{
		Message:  resp.Common.Message,
		VolumeID: resp.VolumeID,
	}
}
