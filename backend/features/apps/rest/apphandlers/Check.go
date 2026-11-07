package apphandlers

import (
	"github.com/SgtFlame/foundation/backend/features/apps/rest/appdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Check handles POST /api/v1/apps/check.
func (ah *AppHandler) Check(c *gin.Context) {
	var req appdto.CheckRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ah.service.Check(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), appdto.CheckResponseFrom(resp))
}
