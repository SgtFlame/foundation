package volumehandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/gin-gonic/gin"
)

// RemoveFolder handles POST /api/v1/kb/volumes/folders/remove.
func (vh *VolumeHandler) RemoveFolder(c *gin.Context) {
	var req volumedto.RemoveFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, volumedto.RemoveFolderResponse{Message: err.Error()})
		return
	}

	resp := vh.service.RemoveFolder(req.ToArgs())
	c.JSON(int(resp.Common.Code), volumedto.RemoveFolderResponseFrom(resp))
}
