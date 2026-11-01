package entitydto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
)

type ListVersionsRequest struct {
	ID string `json:"id"`
}

// ToArgs converts the request to the service's ListVersionsArgs.
func (r ListVersionsRequest) ToArgs(args features.ServiceArgs) entityservice.ListVersionsArgs {
	return entityservice.ListVersionsArgs{ServiceArgs: args, ID: r.ID}
}
