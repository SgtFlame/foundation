package indexdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
)

type ReindexRequest struct {
	Force bool `json:"force,omitempty"`
}

// ToArgs converts the request to the service's ReindexArgs.
func (r ReindexRequest) ToArgs(args features.ServiceArgs) indexservice.ReindexArgs {
	return indexservice.ReindexArgs{ServiceArgs: args, Force: r.Force}
}
