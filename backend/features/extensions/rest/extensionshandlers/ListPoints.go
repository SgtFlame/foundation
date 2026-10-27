package extensionshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/extensions/rest/extensionsdto"
	"github.com/SgtFlame/foundation/backend/features/extensions/services/extensionsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ListPoints handles GET /api/v1/extensions/points; it takes no body.
func (eh *ExtensionsHandler) ListPoints(c *gin.Context) {
	resp := eh.service.ListPoints(extensionsservice.ListPointsArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), extensionsdto.ListPointsResponseFrom(resp))
}
