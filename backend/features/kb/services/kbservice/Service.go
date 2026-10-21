package kbservice

import (
	"github.com/SgtFlame/foundation/backend/features"
)

type Service interface {
	OpenVolumeAsync(args OpenVolumeArgs) *OpenVolumeRequest
	OpenVolume(args OpenVolumeArgs) OpenVolumeResponse
}

type OpenVolumeArgs struct {
	features.ServiceArgs
	Path VolumePath
}

type OpenVolumeRequest struct {
	Response chan *OpenVolumeResponse
}

type OpenVolumeResponse struct {
	Common   features.ServiceResponse
	VolumeID uint64
}
