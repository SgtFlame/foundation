package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type CreateFileRequest struct {
	Path    kbdto.VolumePath `json:"path"`
	Type    string           `json:"type"`
	Content string           `json:"content,omitempty"` // the new file's text; omitted makes it empty
}

// ToArgs converts the request to the service's CreateFileArgs.
func (r CreateFileRequest) ToArgs(args features.ServiceArgs) volumeservice.CreateFileArgs {
	return volumeservice.CreateFileArgs{
		ServiceArgs: args,
		Path:        r.Path.ToVolumePath(),
		Type:        r.Type,
		Content:     r.Content,
	}
}
