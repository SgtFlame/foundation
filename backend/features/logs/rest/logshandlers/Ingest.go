package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Ingest handles POST /api/v1/logs/ingest.
func (lh *LogsHandler) Ingest(c *gin.Context) {
	var req logsdto.IngestRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := lh.service.Ingest(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), logsdto.IngestResponseFrom(resp))
}
