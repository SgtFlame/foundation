package volumehandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/gin-gonic/gin"
)

// MakeFolder handles POST /api/v1/kb/volumes/folders/make.
func (vh *VolumeHandler) MakeFolder(c *gin.Context) {
	var req volumedto.MakeFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, volumedto.MakeFolderResponse{Message: err.Error()})
		return
	}

	resp := vh.service.MakeFolder(req.ToArgs())
	c.JSON(int(resp.Common.Code), volumedto.MakeFolderResponseFrom(resp))
}
