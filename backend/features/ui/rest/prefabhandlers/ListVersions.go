package prefabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/prefabdto"
	"github.com/gin-gonic/gin"
)

// ListVersions handles POST /api/v1/ui/prefabs/versions.
func (ph *PrefabHandler) ListVersions(c *gin.Context) {
	var req prefabdto.ListVersionsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ph.service.ListVersions(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), prefabdto.ListVersionsResponseFrom(resp))
}
