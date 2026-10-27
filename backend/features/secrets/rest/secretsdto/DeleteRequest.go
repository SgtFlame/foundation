package secretsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

type DeleteRequest struct {
	Name string `json:"name" binding:"required"`
}

// ToArgs converts the request to the service's DeleteArgs.
func (r DeleteRequest) ToArgs(args features.ServiceArgs) secretsservice.DeleteArgs {
	return secretsservice.DeleteArgs{ServiceArgs: args, Name: r.Name}
}
