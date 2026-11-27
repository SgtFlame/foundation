package serviceshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/services/rest/servicesdto"
	"github.com/SgtFlame/foundation/backend/features/services/services/servicesservice"
	"github.com/gin-gonic/gin"
)

// Status handles GET /api/v1/services/status; it takes no body.
func (sh *ServicesHandler) Status(c *gin.Context) {
	resp := sh.service.Status(servicesservice.StatusArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), servicesdto.StatusResponseFrom(resp))
}
