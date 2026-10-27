package identitydto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TenantID uint64 `json:"tenantId,string,omitempty"`
}

func (r LoginRequest) ToArgs(args features.ServiceArgs) identityservice.LoginArgs {
	return identityservice.LoginArgs{ServiceArgs: args, Username: r.Username, Password: r.Password, TenantID: r.TenantID}
}
