package identitydto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

type SetupRequest struct {
	TenantName  string `json:"tenantName"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Password    string `json:"password"`
}

func (r SetupRequest) ToArgs(args features.ServiceArgs) identityservice.SetupArgs {
	return identityservice.SetupArgs{ServiceArgs: args, TenantName: r.TenantName, Username: r.Username, DisplayName: r.DisplayName, Email: r.Email, Password: r.Password}
}
