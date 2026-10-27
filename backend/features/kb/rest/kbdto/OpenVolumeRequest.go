package kbdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

type OpenVolumeRequest struct {
	Path VolumePath `json:"path"`
}

// ToArgs converts the request to the service's OpenVolumeArgs.
func (r OpenVolumeRequest) ToArgs(args features.ServiceArgs) kbservice.OpenVolumeArgs {
	return kbservice.OpenVolumeArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
	}
}
