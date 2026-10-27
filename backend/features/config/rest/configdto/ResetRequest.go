package configdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

type ResetRequest struct {
	Section string   `json:"section" binding:"required"`
	Scope   string   `json:"scope"`
	Keys    []string `json:"keys,omitempty"` // leaf paths; every key when empty
}

// ToArgs converts the request to the service's ResetArgs.
func (r ResetRequest) ToArgs(args features.ServiceArgs) configservice.ResetArgs {
	return configservice.ResetArgs{ServiceArgs: args, Section: r.Section, Scope: configservice.Scope(r.Scope), Keys: r.Keys}
}
