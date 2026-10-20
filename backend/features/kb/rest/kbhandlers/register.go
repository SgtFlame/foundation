package kbhandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/gin-gonic/gin"
)

type KnowledgeBaseHandler struct {
	service kbservice.Service
}

func (kbh *KnowledgeBaseHandler) Register(router gin.IRouter, service kbservice.Service) {
	kbh.service = service

	group := router.Group("/kb")

	group.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Hello world from the Knowledge Base"})
	})

	group.POST("/vaults/open", kbh.OpenVault)
}
