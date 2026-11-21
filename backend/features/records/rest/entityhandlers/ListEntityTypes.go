package entityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/entitydto"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ListEntityTypes handles GET /api/v1/records/entities; it takes no body.
func (eh *EntityHandler) ListEntityTypes(c *gin.Context) {
	resp := eh.service.ListEntityTypes(entityservice.ListEntityTypesArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), entitydto.ListEntityTypesResponseFrom(resp))
}
