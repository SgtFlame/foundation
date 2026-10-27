package volumehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumedto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// MakeFolder handles POST /api/v1/kb/volumes/folders/make.
func (vh *VolumeHandler) MakeFolder(c *gin.Context) {
	var req volumedto.MakeFolderRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := vh.service.MakeFolder(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), volumedto.MakeFolderResponseFrom(resp))
}
