package confighandlers

import (
	"github.com/SgtFlame/foundation/backend/features/config/rest/configdto"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Sections handles GET /api/v1/config/sections; it takes no body.
func (ch *ConfigHandler) Sections(c *gin.Context) {
	resp := ch.service.Sections(configservice.SectionsArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), configdto.SectionsResponseFrom(resp))
}
