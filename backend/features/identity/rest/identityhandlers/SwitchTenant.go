package identityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// SwitchTenant handles POST /api/v1/identity/switch-tenant.
func (ih *IdentityHandler) SwitchTenant(c *gin.Context) {
	var req identitydto.SwitchTenantRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	in := req.ToArgs(args)
	in.Token = refreshToken(c)
	resp := ih.service.SwitchTenant(in)
	if resp.Common.Code >= 300 {
		c.JSON(int(resp.Common.Code), identitydto.SessionResponseFrom(resp.Common, resp.Session, nil))
		return
	}
	setSession(c, resp.Session)
	account := identitydto.AccountFrom(resp.Account)
	c.JSON(int(resp.Common.Code), identitydto.SessionResponseFrom(resp.Common, resp.Session, &account))
}
