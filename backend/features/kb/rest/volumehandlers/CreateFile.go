package volumehandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/gin-gonic/gin"
)

// CreateFile handles POST /api/v1/kb/volumes/files/create.
func (vh *VolumeHandler) CreateFile(c *gin.Context) {
	var req volumedto.CreateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, volumedto.CreateFileResponse{Message: err.Error()})
		return
	}

	resp := vh.service.CreateFile(req.ToArgs())
	c.JSON(int(resp.Common.Code), volumedto.CreateFileResponseFrom(resp))
}
