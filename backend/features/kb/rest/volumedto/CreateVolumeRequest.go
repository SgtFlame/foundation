package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type CreateVolumeRequest struct {
	Tenant uint64           `json:"tenant,string,omitempty"`
	Path   kbdto.VolumePath `json:"path"`
}

// ToArgs converts the request to the service's CreateVolumeArgs.
func (r CreateVolumeRequest) ToArgs() volumeservice.CreateVolumeArgs {
	return volumeservice.CreateVolumeArgs{
		ServiceArgs: features.ServiceArgs{Tenant: r.Tenant},
		Path:        r.Path.ToVolumePath(),
	}
}
