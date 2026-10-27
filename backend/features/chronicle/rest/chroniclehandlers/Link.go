package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Link handles POST /api/v1/chronicle/link.
func (ch *ChronicleHandler) Link(c *gin.Context) {
	var req chronicledto.LinkRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Link(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.LinkResponseFrom(resp))
}
