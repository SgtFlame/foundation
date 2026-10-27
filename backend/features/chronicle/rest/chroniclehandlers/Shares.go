package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Shares handles POST /api/v1/chronicle/shares.
func (ch *ChronicleHandler) Shares(c *gin.Context) {
	var req chronicledto.SharesRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Shares(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.SharesResponseFrom(resp))
}
