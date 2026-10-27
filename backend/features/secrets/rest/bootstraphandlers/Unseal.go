package bootstraphandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/bootstrapdto"
	"github.com/gin-gonic/gin"
)

// Unseal handles POST /api/v1/bootstrap/unseal.
func (bh *BootstrapHandler) Unseal(c *gin.Context) {
	var req bootstrapdto.UnsealRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := bh.service.Unseal(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), bootstrapdto.UnsealResponseFrom(resp))
}
