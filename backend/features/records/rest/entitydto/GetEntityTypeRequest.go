package entitydto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
)

type GetEntityTypeRequest struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"` // the last when left out
}

// ToArgs converts the request to the service's GetEntityTypeArgs.
func (r GetEntityTypeRequest) ToArgs(args features.ServiceArgs) entityservice.GetEntityTypeArgs {
	return entityservice.GetEntityTypeArgs{ServiceArgs: args, ID: r.ID, Version: r.Version}
}
