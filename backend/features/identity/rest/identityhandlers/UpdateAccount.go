package identityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// UpdateAccount handles POST /api/v1/identity/account. A changed password
// comes with a new session.
func (ih *IdentityHandler) UpdateAccount(c *gin.Context) {
	var req identitydto.UpdateAccountRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ih.service.UpdateAccount(req.ToArgs(args))
	if resp.Common.Code >= 300 {
		c.JSON(int(resp.Common.Code), identitydto.SessionResponseFrom(resp.Common, identity.Session{}, nil))
		return
	}
	var session identity.Session
	if resp.Session != nil {
		session = *resp.Session
		setSession(c, session)
	}
	account := identitydto.AccountFrom(resp.Account)
	c.JSON(int(resp.Common.Code), identitydto.SessionResponseFrom(resp.Common, session, &account))
}
