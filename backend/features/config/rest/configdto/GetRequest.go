package configdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

type GetRequest struct {
	Section string `json:"section" binding:"required"`
	Scope   string `json:"scope"` // app (the default), tenant or user
}

// ToArgs converts the request to the service's GetArgs.
func (r GetRequest) ToArgs(args features.ServiceArgs) configservice.GetArgs {
	return configservice.GetArgs{ServiceArgs: args, Section: r.Section, Scope: configservice.Scope(r.Scope)}
}
