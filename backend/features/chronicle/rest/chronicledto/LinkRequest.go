package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type LinkRequest struct {
	FromID uint64 `json:"fromId,string"`
	ToID   uint64 `json:"toId,string"`
	Remove bool   `json:"remove,omitempty"`
}

// ToArgs converts the request to the service's LinkArgs.
func (r LinkRequest) ToArgs(args features.ServiceArgs) chronicleservice.LinkArgs {
	return chronicleservice.LinkArgs{ServiceArgs: args, FromID: r.FromID, ToID: r.ToID, Remove: r.Remove}
}
