package embedhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/ai/rest/embeddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Describe handles POST /api/v1/ai/embed/describe.
func (eh *EmbedHandler) Describe(c *gin.Context) {
	var req embeddto.DescribeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := eh.service.Describe(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), embeddto.DescribeResponseFrom(resp))
}
