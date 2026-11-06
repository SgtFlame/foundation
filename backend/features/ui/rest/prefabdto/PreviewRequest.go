package prefabdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

type PreviewRequest struct {
	Prefab string         `json:"prefab"`
	Props  map[string]any `json:"props,omitempty"` // its preview data when left out
	Draft  bool           `json:"draft,omitempty"` // its draft, not its last published version
}

// ToArgs converts the request to the service's PreviewArgs.
func (r PreviewRequest) ToArgs(args features.ServiceArgs) prefabservice.PreviewArgs {
	return prefabservice.PreviewArgs{ServiceArgs: args, Prefab: r.Prefab, Props: r.Props, Draft: r.Draft}
}
