package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Delete handles POST /api/v1/chronicle/delete.
func (ch *ChronicleHandler) Delete(c *gin.Context) {
	var req chronicledto.DeleteRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Delete(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.DeleteResponseFrom(resp))
}
