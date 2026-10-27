package cognitiondto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
)

type GetGraphRequest struct {
	ID string `json:"id"`
}

// ToArgs converts the request to the service's GetGraphArgs.
func (r GetGraphRequest) ToArgs(args features.ServiceArgs) cognitionservice.GetGraphArgs {
	return cognitionservice.GetGraphArgs{ServiceArgs: args, ID: r.ID}
}
