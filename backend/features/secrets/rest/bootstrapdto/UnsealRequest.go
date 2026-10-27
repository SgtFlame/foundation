package bootstrapdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
)

// UnsealRequest carries a response-wrapped SecretID and, unless serve has
// --vault-role-id, the AppRole's RoleID.
type UnsealRequest struct {
	Token  string `json:"token" binding:"required"`
	RoleID string `json:"roleId"`
}

// ToArgs converts the request to the service's UnsealArgs.
func (r UnsealRequest) ToArgs(args features.ServiceArgs) bootstrapservice.UnsealArgs {
	return bootstrapservice.UnsealArgs{ServiceArgs: args, Token: r.Token, RoleID: r.RoleID}
}
