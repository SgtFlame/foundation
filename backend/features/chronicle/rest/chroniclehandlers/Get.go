package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Get handles POST /api/v1/chronicle/get.
func (ch *ChronicleHandler) Get(c *gin.Context) {
	var req chronicledto.GetRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Get(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.GetResponseFrom(resp))
}
