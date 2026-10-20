package kbhandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
	"github.com/gin-gonic/gin"
)

// OpenVault handles POST /api/v1/kb/vaults/open.
func (kbh *KnowledgeBaseHandler) OpenVault(c *gin.Context) {
	var req kbdto.OpenVaultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, kbdto.OpenVaultResponse{Message: err.Error()})
		return
	}

	resp := kbh.service.OpenVault(req.ToArgs())
	c.JSON(int(resp.Common.Code), kbdto.OpenVaultResponseFrom(resp))
}
