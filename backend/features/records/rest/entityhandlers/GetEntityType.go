package entityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/entitydto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// GetEntityType handles POST /api/v1/records/entities/get.
func (eh *EntityHandler) GetEntityType(c *gin.Context) {
	var req entitydto.GetEntityTypeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := eh.service.GetEntityType(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), entitydto.GetEntityTypeResponseFrom(resp))
}
