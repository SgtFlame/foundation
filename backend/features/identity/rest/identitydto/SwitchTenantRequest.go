package identitydto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

type SwitchTenantRequest struct {
	TenantID uint64 `json:"tenantId,string"`
}

// ToArgs converts the request; the handler adds the refresh cookie.
func (r SwitchTenantRequest) ToArgs(args features.ServiceArgs) identityservice.SwitchTenantArgs {
	return identityservice.SwitchTenantArgs{ServiceArgs: args, TenantID: r.TenantID}
}
