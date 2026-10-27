package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type MakeFolderRequest struct {
	Path kbdto.VolumePath `json:"path"`
}

// ToArgs converts the request to the service's MakeFolderArgs.
func (r MakeFolderRequest) ToArgs(args features.ServiceArgs) volumeservice.MakeFolderArgs {
	return volumeservice.MakeFolderArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
	}
}
