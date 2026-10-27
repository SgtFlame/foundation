package usagehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/usage/rest/usagedto"
	"github.com/gin-gonic/gin"
)

// Records handles POST /api/v1/usage/records.
func (uh *UsageHandler) Records(c *gin.Context) {
	var req usagedto.RecordsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := uh.service.Records(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), usagedto.RecordsResponseFrom(resp))
}
