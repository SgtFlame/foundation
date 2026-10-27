package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// FieldValues handles POST /api/v1/logs/fields/values.
func (lh *LogsHandler) FieldValues(c *gin.Context) {
	var req logsdto.FieldValuesRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := lh.service.FieldValues(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), logsdto.FieldValuesResponseFrom(resp))
}
