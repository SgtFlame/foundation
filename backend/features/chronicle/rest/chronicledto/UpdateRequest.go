package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type UpdateRequest struct {
	ID uint64 `json:"id,string"`
	EntryInput
	Accept   bool `json:"accept,omitempty"`   // makes a proposed entry active
	Reviewed bool `json:"reviewed,omitempty"` // clears the review flag
}

// ToArgs converts the request to the service's UpdateArgs.
func (r UpdateRequest) ToArgs(args features.ServiceArgs) chronicleservice.UpdateArgs {
	return chronicleservice.UpdateArgs{ServiceArgs: args, ID: r.ID, EntryInput: r.EntryInput.toInput(), Accept: r.Accept, Reviewed: r.Reviewed}
}
