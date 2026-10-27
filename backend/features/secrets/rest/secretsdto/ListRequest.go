package secretsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

type ListRequest struct {
	Prefix string `json:"prefix"`
}

// ToArgs converts the request to the service's ListArgs.
func (r ListRequest) ToArgs(args features.ServiceArgs) secretsservice.ListArgs {
	return secretsservice.ListArgs{ServiceArgs: args, Prefix: r.Prefix}
}
