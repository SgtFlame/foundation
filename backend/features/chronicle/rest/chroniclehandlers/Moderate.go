package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Moderate handles POST /api/v1/chronicle/moderate.
func (ch *ChronicleHandler) Moderate(c *gin.Context) {
	var req chronicledto.ModerateRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Moderate(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.ModerateResponseFrom(resp))
}
