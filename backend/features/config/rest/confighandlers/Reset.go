package confighandlers

import (
	"github.com/SgtFlame/foundation/backend/features/config/rest/configdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Reset handles POST /api/v1/config/reset.
func (ch *ConfigHandler) Reset(c *gin.Context) {
	var req configdto.ResetRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Reset(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), configdto.ResetResponseFrom(resp))
}
