package cognitionhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/rest/cognitiondto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// GetTrace handles POST /api/v1/cognition/trace.
func (ch *CognitionHandler) GetTrace(c *gin.Context) {
	var req cognitiondto.GetTraceRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.GetTrace(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), cognitiondto.GetTraceResponseFrom(resp))
}
