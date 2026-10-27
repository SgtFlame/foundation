package cognitionhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/rest/cognitiondto"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ListGraphs handles GET /api/v1/cognition/graphs; it takes no body.
func (ch *CognitionHandler) ListGraphs(c *gin.Context) {
	resp := ch.service.ListGraphs(cognitionservice.ListGraphsArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), cognitiondto.ListGraphsResponseFrom(resp))
}
