package cognitiondto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
)

type GetTraceRequest struct {
	RunID uint64 `json:"runId,string"`
}

// ToArgs converts the request to the service's GetTraceArgs.
func (r GetTraceRequest) ToArgs(args features.ServiceArgs) cognitionservice.GetTraceArgs {
	return cognitionservice.GetTraceArgs{ServiceArgs: args, RunID: r.RunID}
}
