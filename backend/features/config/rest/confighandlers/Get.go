package confighandlers

import (
	"github.com/SgtFlame/foundation/backend/features/config/rest/configdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Get handles POST /api/v1/config/get.
func (ch *ConfigHandler) Get(c *gin.Context) {
	var req configdto.GetRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Get(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), configdto.GetResponseFrom(resp))
}
