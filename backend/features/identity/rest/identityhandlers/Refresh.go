package identityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Refresh handles POST /api/v1/identity/refresh: the refresh cookie is the
// request. A session that has ended loses its cookies.
func (ih *IdentityHandler) Refresh(c *gin.Context) {
	resp := ih.service.Refresh(identityservice.RefreshArgs{ServiceArgs: rest.Args(c), Token: refreshToken(c)})
	if resp.Common.Code < 300 {
		setSession(c, resp.Session)
	} else {
		clearSession(c)
	}
	c.JSON(int(resp.Common.Code), identitydto.SessionResponseFrom(resp.Common, resp.Session, nil))
}
