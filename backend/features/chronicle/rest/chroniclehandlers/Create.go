package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Create handles POST /api/v1/chronicle/create.
func (ch *ChronicleHandler) Create(c *gin.Context) {
	var req chronicledto.CreateRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Create(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.EntryResponseFrom(resp))
}
