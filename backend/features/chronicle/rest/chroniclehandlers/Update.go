package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Update handles POST /api/v1/chronicle/update.
func (ch *ChronicleHandler) Update(c *gin.Context) {
	var req chronicledto.UpdateRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Update(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.EntryResponseFrom(resp))
}
