package usagedto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

type CaptureRequest struct {
	RecordID uint64 `json:"recordId,string"`
}

// ToArgs converts the request to the service's CaptureArgs.
func (r CaptureRequest) ToArgs(args features.ServiceArgs) usageservice.CaptureArgs {
	return usageservice.CaptureArgs{ServiceArgs: args, RecordID: r.RecordID}
}
