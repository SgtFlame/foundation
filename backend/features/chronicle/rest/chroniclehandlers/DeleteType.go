package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// DeleteType handles POST /api/v1/chronicle/types/delete.
func (ch *ChronicleHandler) DeleteType(c *gin.Context) {
	var req chronicledto.DeleteTypeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ch.service.DeleteType(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.DeleteTypeResponseFrom(resp))
}
