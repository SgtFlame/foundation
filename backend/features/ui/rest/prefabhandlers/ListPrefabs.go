package prefabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/prefabdto"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
	"github.com/gin-gonic/gin"
)

// ListPrefabs handles GET /api/v1/ui/prefabs; it takes no body.
func (ph *PrefabHandler) ListPrefabs(c *gin.Context) {
	resp := ph.service.ListPrefabs(prefabservice.ListPrefabsArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), prefabdto.ListPrefabsResponseFrom(resp))
}
