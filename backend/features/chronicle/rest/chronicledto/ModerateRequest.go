package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type ModerateRequest struct {
	ShareID uint64 `json:"shareId,string"`
	Approve bool   `json:"approve"`
	Note    string `json:"note,omitempty"`
}

// ToArgs converts the request to the service's ModerateArgs.
func (r ModerateRequest) ToArgs(args features.ServiceArgs) chronicleservice.ModerateArgs {
	return chronicleservice.ModerateArgs{ServiceArgs: args, ShareID: r.ShareID, Approve: r.Approve, Note: r.Note}
}
