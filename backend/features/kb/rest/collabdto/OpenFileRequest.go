package collabdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

type OpenFileRequest struct {
	Path kbdto.VolumePath `json:"path"`
	Type string           `json:"type"`
}

// ToArgs converts the request to the service's OpenFileArgs.
func (r OpenFileRequest) ToArgs(args features.ServiceArgs) collabservice.OpenFileArgs {
	return collabservice.OpenFileArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
		Type:        r.Type,
	}
}
