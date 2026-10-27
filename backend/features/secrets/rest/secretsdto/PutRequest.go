package secretsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

type PutRequest struct {
	Name  string `json:"name" binding:"required"`
	Value string `json:"value" binding:"required"`
}

// ToArgs converts the request to the service's PutArgs.
func (r PutRequest) ToArgs(args features.ServiceArgs) secretsservice.PutArgs {
	return secretsservice.PutArgs{ServiceArgs: args, Name: r.Name, Value: r.Value}
}
