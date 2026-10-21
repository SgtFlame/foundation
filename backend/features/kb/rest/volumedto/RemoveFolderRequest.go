package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type RemoveFolderRequest struct {
	Tenant uint64           `json:"tenant,string,omitempty"`
	Path   kbdto.VolumePath `json:"path"`
}

// ToArgs converts the request to the service's RemoveFolderArgs.
func (r RemoveFolderRequest) ToArgs() volumeservice.RemoveFolderArgs {
	return volumeservice.RemoveFolderArgs{
		ServiceArgs: features.ServiceArgs{Tenant: r.Tenant},
		Path:        r.Path.ToVolumePath(),
	}
}
