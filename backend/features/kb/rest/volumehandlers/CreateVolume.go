package volumehandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/gin-gonic/gin"
)

// CreateVolume handles POST /api/v1/kb/volumes/create.
func (vh *VolumeHandler) CreateVolume(c *gin.Context) {
	var req volumedto.CreateVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, volumedto.CreateVolumeResponse{Message: err.Error()})
		return
	}

	resp := vh.service.CreateVolume(req.ToArgs())
	c.JSON(int(resp.Common.Code), volumedto.CreateVolumeResponseFrom(resp))
}
