package appdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
)

type CheckRequest struct {
	Document apps.Document `json:"document"`
}

// ToArgs converts the request to the service's CheckArgs.
func (r CheckRequest) ToArgs(args features.ServiceArgs) appservice.CheckArgs {
	return appservice.CheckArgs{ServiceArgs: args, Document: r.Document}
}
