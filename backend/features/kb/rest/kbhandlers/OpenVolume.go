package kbhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// OpenVolume handles POST /api/v1/kb/volumes/open.
func (kbh *KnowledgeBaseHandler) OpenVolume(c *gin.Context) {
	var req kbdto.OpenVolumeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := kbh.service.OpenVolume(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), kbdto.OpenVolumeResponseFrom(resp))
}
