package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Streams handles POST /api/v1/logs/streams.
func (lh *LogsHandler) Streams(c *gin.Context) {
	var req logsdto.StreamsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := lh.service.Streams(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), logsdto.StreamsResponseFrom(resp))
}
