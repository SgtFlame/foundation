package usagehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/usage/rest/usagedto"
	"github.com/gin-gonic/gin"
)

// Capture handles POST /api/v1/usage/capture.
func (uh *UsageHandler) Capture(c *gin.Context) {
	var req usagedto.CaptureRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := uh.service.Capture(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), usagedto.CaptureResponseFrom(resp))
}
