package kbdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

type OpenVolumeRequest struct {
	Tenant uint64     `json:"tenant,string,omitempty"`
	Path   VolumePath `json:"path"`
}

// ToArgs converts the request to the service's OpenVolumeArgs.
func (r OpenVolumeRequest) ToArgs() kbservice.OpenVolumeArgs {
	return kbservice.OpenVolumeArgs{
		ServiceArgs: features.ServiceArgs{Tenant: r.Tenant},
		Path:        r.Path.ToVolumePath(),
	}
}
