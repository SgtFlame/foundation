package apphandlers

import (
	"github.com/SgtFlame/foundation/backend/features/apps/rest/appdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Import handles POST /api/v1/apps/import.
func (ah *AppHandler) Import(c *gin.Context) {
	var req appdto.ImportRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ah.service.Import(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), appdto.ImportResponseFrom(resp))
}
