package apphandlers

import (
	"github.com/SgtFlame/foundation/backend/features/apps/rest/appdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Export handles POST /api/v1/apps/export.
func (ah *AppHandler) Export(c *gin.Context) {
	var req appdto.ExportRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ah.service.Export(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), appdto.ExportResponseFrom(resp))
}
