package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Hits handles POST /api/v1/logs/hits.
func (lh *LogsHandler) Hits(c *gin.Context) {
	var req logsdto.HitsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := lh.service.Hits(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), logsdto.HitsResponseFrom(resp))
}
