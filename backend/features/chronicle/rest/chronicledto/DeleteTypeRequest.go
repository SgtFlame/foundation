package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// DeleteTypeRequest removes a type no entry uses.
type DeleteTypeRequest struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
}

// ToArgs converts the request to the service's DeleteTypeArgs.
func (r DeleteTypeRequest) ToArgs(args features.ServiceArgs) chronicleservice.DeleteTypeArgs {
	return chronicleservice.DeleteTypeArgs{ServiceArgs: args, ID: r.ID, Scope: r.Scope}
}
