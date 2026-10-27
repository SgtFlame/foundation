package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type SharesRequest struct {
	Queue bool `json:"queue,omitempty"` // the requests waiting for the principal to moderate
}

// ToArgs converts the request to the service's SharesArgs.
func (r SharesRequest) ToArgs(args features.ServiceArgs) chronicleservice.SharesArgs {
	return chronicleservice.SharesArgs{ServiceArgs: args, Queue: r.Queue}
}
