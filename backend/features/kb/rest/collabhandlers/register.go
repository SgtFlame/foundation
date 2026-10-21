package collabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/gin-gonic/gin"
)

type CollabHandler struct {
	service collabservice.Service
}

func (ch *CollabHandler) Register(router gin.IRouter, service collabservice.Service) {
	ch.service = service

	group := router.Group("/kb/collab")

	group.POST("/files/open", ch.OpenFile)
	group.GET("/files/:fileId", ch.Collaborate) // WebSocket
}
