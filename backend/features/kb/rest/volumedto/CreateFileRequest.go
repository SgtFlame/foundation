package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type CreateFileRequest struct {
	Tenant uint64           `json:"tenant,string,omitempty"`
	Path   kbdto.VolumePath `json:"path"`
	Type   string           `json:"type"`
}

// ToArgs converts the request to the service's CreateFileArgs.
func (r CreateFileRequest) ToArgs() volumeservice.CreateFileArgs {
	return volumeservice.CreateFileArgs{
		ServiceArgs: features.ServiceArgs{Tenant: r.Tenant},
		Path:        r.Path.ToVolumePath(),
		Type:        r.Type,
	}
}
