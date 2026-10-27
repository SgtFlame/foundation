package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// FieldNames handles POST /api/v1/logs/fields/names.
func (lh *LogsHandler) FieldNames(c *gin.Context) {
	var req logsdto.FieldNamesRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := lh.service.FieldNames(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), logsdto.FieldNamesResponseFrom(resp))
}
