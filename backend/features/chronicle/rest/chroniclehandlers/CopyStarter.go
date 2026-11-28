package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// CopyStarter handles POST /api/v1/chronicle/types/copy-starter.
func (ch *ChronicleHandler) CopyStarter(c *gin.Context) {
	var req chronicledto.CopyStarterRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ch.service.CopyStarter(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.TypesResponseFrom(resp))
}
