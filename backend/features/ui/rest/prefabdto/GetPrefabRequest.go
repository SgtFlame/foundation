package prefabdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

type GetPrefabRequest struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"` // the last when left out
}

// ToArgs converts the request to the service's GetPrefabArgs.
func (r GetPrefabRequest) ToArgs(args features.ServiceArgs) prefabservice.GetPrefabArgs {
	return prefabservice.GetPrefabArgs{ServiceArgs: args, ID: r.ID, Version: r.Version}
}
