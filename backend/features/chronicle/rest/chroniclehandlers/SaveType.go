package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// SaveType handles POST /api/v1/chronicle/types/save.
func (ch *ChronicleHandler) SaveType(c *gin.Context) {
	var req chronicledto.SaveTypeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ch.service.SaveType(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.SaveTypeResponseFrom(resp))
}
