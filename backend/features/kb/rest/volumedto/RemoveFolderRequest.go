package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type RemoveFolderRequest struct {
	Path kbdto.VolumePath `json:"path"`
}

// ToArgs converts the request to the service's RemoveFolderArgs.
func (r RemoveFolderRequest) ToArgs(args features.ServiceArgs) volumeservice.RemoveFolderArgs {
	return volumeservice.RemoveFolderArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
	}
}
