package prefabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/prefabdto"
	"github.com/gin-gonic/gin"
)

// Preview handles POST /api/v1/ui/prefabs/preview.
func (ph *PrefabHandler) Preview(c *gin.Context) {
	var req prefabdto.PreviewRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ph.service.Preview(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), prefabdto.PreviewResponseFrom(resp))
}
