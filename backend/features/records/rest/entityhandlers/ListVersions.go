package entityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/entitydto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ListVersions handles POST /api/v1/records/entities/versions.
func (eh *EntityHandler) ListVersions(c *gin.Context) {
	var req entitydto.ListVersionsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := eh.service.ListVersions(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), entitydto.ListVersionsResponseFrom(resp))
}
