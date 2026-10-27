package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type ShareRequest struct {
	ID     uint64 `json:"id,string"`
	Target string `json:"target"` // tenant, global, or "" to stop sharing
	Note   string `json:"note,omitempty"`
}

// ToArgs converts the request to the service's ShareArgs.
func (r ShareRequest) ToArgs(args features.ServiceArgs) chronicleservice.ShareArgs {
	return chronicleservice.ShareArgs{ServiceArgs: args, ID: r.ID, Target: r.Target, Note: r.Note}
}
