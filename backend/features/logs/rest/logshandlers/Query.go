package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Query handles POST /api/v1/logs/query.
func (lh *LogsHandler) Query(c *gin.Context) {
	var req logsdto.QueryRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := lh.service.Query(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), logsdto.QueryResponseFrom(resp))
}
