package prefabdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

type ListVersionsRequest struct {
	ID string `json:"id"`
}

// ToArgs converts the request to the service's ListVersionsArgs.
func (r ListVersionsRequest) ToArgs(args features.ServiceArgs) prefabservice.ListVersionsArgs {
	return prefabservice.ListVersionsArgs{ServiceArgs: args, ID: r.ID}
}
