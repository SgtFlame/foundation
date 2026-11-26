package prefabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/prefabdto"
	"github.com/gin-gonic/gin"
)

// GetPrefab handles POST /api/v1/ui/prefabs/get.
func (ph *PrefabHandler) GetPrefab(c *gin.Context) {
	var req prefabdto.GetPrefabRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ph.service.GetPrefab(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), prefabdto.GetPrefabResponseFrom(resp))
}
