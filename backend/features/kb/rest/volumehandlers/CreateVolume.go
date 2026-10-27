package volumehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// CreateVolume handles POST /api/v1/kb/volumes/create.
func (vh *VolumeHandler) CreateVolume(c *gin.Context) {
	var req volumedto.CreateVolumeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := vh.service.CreateVolume(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), volumedto.CreateVolumeResponseFrom(resp))
}
