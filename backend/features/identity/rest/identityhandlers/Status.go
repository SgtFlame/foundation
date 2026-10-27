package identityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Status handles GET /api/v1/identity/status.
func (ih *IdentityHandler) Status(c *gin.Context) {
	resp := ih.service.Status(identityservice.StatusArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), identitydto.StatusResponseFrom(resp))
}
