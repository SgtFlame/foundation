package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type CreateVolumeRequest struct {
	Path kbdto.VolumePath `json:"path"`
}

// ToArgs converts the request to the service's CreateVolumeArgs.
func (r CreateVolumeRequest) ToArgs(args features.ServiceArgs) volumeservice.CreateVolumeArgs {
	return volumeservice.CreateVolumeArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
	}
}
