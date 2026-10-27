package indexdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
)

type IndexFileRequest struct {
	Path kbdto.VolumePath `json:"path"`
	Type string           `json:"type"`
}

// ToArgs converts the request to the service's IndexFileArgs.
func (r IndexFileRequest) ToArgs(args features.ServiceArgs) indexservice.IndexFileArgs {
	return indexservice.IndexFileArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
		Type:        r.Type,
	}
}
