package volumehandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/gin-gonic/gin"
)

// ListFolder handles POST /api/v1/kb/volumes/folders/list.
func (vh *VolumeHandler) ListFolder(c *gin.Context) {
	var req volumedto.ListFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, volumedto.ListFolderResponse{Message: err.Error()})
		return
	}

	resp := vh.service.ListFolder(req.ToArgs())
	c.JSON(int(resp.Common.Code), volumedto.ListFolderResponseFrom(resp))
}
