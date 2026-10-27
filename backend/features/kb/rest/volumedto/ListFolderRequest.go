package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type ListFolderRequest struct {
	Path kbdto.VolumePath `json:"path"`
}

// ToArgs converts the request to the service's ListFolderArgs.
func (r ListFolderRequest) ToArgs(args features.ServiceArgs) volumeservice.ListFolderArgs {
	return volumeservice.ListFolderArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
	}
}
