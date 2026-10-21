package collabdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

type OpenFileRequest struct {
	Tenant uint64           `json:"tenant,string,omitempty"`
	Path   kbdto.VolumePath `json:"path"`
	Type   string           `json:"type"`
}

// ToArgs converts the request to the service's OpenFileArgs.
func (r OpenFileRequest) ToArgs() collabservice.OpenFileArgs {
	return collabservice.OpenFileArgs{
		ServiceArgs: features.ServiceArgs{Tenant: r.Tenant},
		Path:        r.Path.ToVolumePath(),
		Type:        r.Type,
	}
}
