package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type DeleteRequest struct {
	ID uint64 `json:"id,string"`
}

// ToArgs converts the request to the service's DeleteArgs.
func (r DeleteRequest) ToArgs(args features.ServiceArgs) chronicleservice.DeleteArgs {
	return chronicleservice.DeleteArgs{ServiceArgs: args, ID: r.ID}
}
