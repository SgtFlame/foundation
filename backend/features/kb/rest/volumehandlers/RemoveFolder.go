package volumehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// RemoveFolder handles POST /api/v1/kb/volumes/folders/remove.
func (vh *VolumeHandler) RemoveFolder(c *gin.Context) {
	var req volumedto.RemoveFolderRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := vh.service.RemoveFolder(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), volumedto.RemoveFolderResponseFrom(resp))
}
