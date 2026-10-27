package identityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Logout handles POST /api/v1/identity/logout, clearing the cookies even
// when the session could not be ended.
func (ih *IdentityHandler) Logout(c *gin.Context) {
	resp := ih.service.Logout(identityservice.LogoutArgs{ServiceArgs: rest.Args(c), Token: refreshToken(c)})
	clearSession(c)
	c.JSON(int(resp.Common.Code), rest.StatusFrom(resp.Common))
}
