package uihandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/uidto"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
	"github.com/gin-gonic/gin"
)

// Components handles GET /api/v1/ui/components.
func (uh *UiHandler) Components(c *gin.Context) {
	resp := uh.service.Components(uiservice.ComponentsArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), uidto.ComponentsResponseFrom(resp))
}
