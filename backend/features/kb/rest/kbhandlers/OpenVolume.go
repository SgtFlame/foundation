package kbhandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/gin-gonic/gin"
)

// OpenVolume handles POST /api/v1/kb/volumes/open.
func (kbh *KnowledgeBaseHandler) OpenVolume(c *gin.Context) {
	var req kbdto.OpenVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, kbdto.OpenVolumeResponse{Message: err.Error()})
		return
	}

	resp := kbh.service.OpenVolume(req.ToArgs())
	c.JSON(int(resp.Common.Code), kbdto.OpenVolumeResponseFrom(resp))
}
