package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Share handles POST /api/v1/chronicle/share.
func (ch *ChronicleHandler) Share(c *gin.Context) {
	var req chronicledto.ShareRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Share(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.ShareResponseFrom(resp))
}
