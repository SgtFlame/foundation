package bootstraphandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/bootstrapdto"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
	"github.com/gin-gonic/gin"
)

// Status handles GET /api/v1/bootstrap/status.
func (bh *BootstrapHandler) Status(c *gin.Context) {
	resp := bh.service.Status(bootstrapservice.StatusArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), bootstrapdto.StatusResponseFrom(resp))
}
