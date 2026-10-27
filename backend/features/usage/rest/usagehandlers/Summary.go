package usagehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/usage/rest/usagedto"
	"github.com/gin-gonic/gin"
)

// Summary handles POST /api/v1/usage/summary.
func (uh *UsageHandler) Summary(c *gin.Context) {
	var req usagedto.SummaryRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := uh.service.Summary(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), usagedto.SummaryResponseFrom(resp))
}
