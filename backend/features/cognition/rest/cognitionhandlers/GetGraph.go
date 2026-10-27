package cognitionhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/rest/cognitiondto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// GetGraph handles POST /api/v1/cognition/graph.
func (ch *CognitionHandler) GetGraph(c *gin.Context) {
	var req cognitiondto.GetGraphRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.GetGraph(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), cognitiondto.GetGraphResponseFrom(resp))
}
