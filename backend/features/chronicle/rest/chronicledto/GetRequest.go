package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type GetRequest struct {
	ID uint64 `json:"id,string"`
}

// ToArgs converts the request to the service's GetArgs.
func (r GetRequest) ToArgs(args features.ServiceArgs) chronicleservice.GetArgs {
	return chronicleservice.GetArgs{ServiceArgs: args, ID: r.ID}
}
