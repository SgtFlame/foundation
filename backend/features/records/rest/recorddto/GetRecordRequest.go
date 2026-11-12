package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

type GetRecordRequest struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
}

// ToArgs converts the request to the service's GetArgs.
func (r GetRecordRequest) ToArgs(args features.ServiceArgs) recordservice.GetArgs {
	return recordservice.GetArgs{ServiceArgs: args, Entity: r.Entity, ID: r.ID}
}
