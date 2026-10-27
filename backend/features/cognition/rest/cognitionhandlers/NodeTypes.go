package cognitionhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/rest/cognitiondto"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// NodeTypes handles GET /api/v1/cognition/node-types; it takes no body.
func (ch *CognitionHandler) NodeTypes(c *gin.Context) {
	resp := ch.service.NodeTypes(cognitionservice.NodeTypesArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), cognitiondto.NodeTypesResponseFrom(resp))
}
