package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Status handles GET /api/v1/logs/status.
func (lh *LogsHandler) Status(c *gin.Context) {
	resp := lh.service.Status(logsservice.StatusArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), logsdto.StatusResponseFrom(resp))
}
