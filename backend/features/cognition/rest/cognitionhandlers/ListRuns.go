package cognitionhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/rest/cognitiondto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ListRuns handles POST /api/v1/cognition/runs.
func (ch *CognitionHandler) ListRuns(c *gin.Context) {
	var req cognitiondto.ListRunsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.ListRuns(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), cognitiondto.ListRunsResponseFrom(resp))
}
