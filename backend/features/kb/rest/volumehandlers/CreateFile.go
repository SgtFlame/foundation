package volumehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// CreateFile handles POST /api/v1/kb/volumes/files/create.
func (vh *VolumeHandler) CreateFile(c *gin.Context) {
	var req volumedto.CreateFileRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := vh.service.CreateFile(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), volumedto.CreateFileResponseFrom(resp))
}
