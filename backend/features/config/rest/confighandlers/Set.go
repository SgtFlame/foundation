package confighandlers

import (
	"github.com/SgtFlame/foundation/backend/features/config/rest/configdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Set handles POST /api/v1/config/set.
func (ch *ConfigHandler) Set(c *gin.Context) {
	var req configdto.SetRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Set(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), configdto.SetResponseFrom(resp))
}
