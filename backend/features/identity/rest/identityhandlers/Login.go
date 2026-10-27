package identityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Login handles POST /api/v1/identity/login.
func (ih *IdentityHandler) Login(c *gin.Context) {
	var req identitydto.LoginRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ih.service.Login(req.ToArgs(args))
	if resp.Common.Code >= 300 {
		c.JSON(int(resp.Common.Code), identitydto.SessionResponseFrom(resp.Common, resp.Session, nil))
		return
	}
	setSession(c, resp.Session)
	account := identitydto.AccountFrom(resp.Account)
	c.JSON(int(resp.Common.Code), identitydto.SessionResponseFrom(resp.Common, resp.Session, &account))
}
