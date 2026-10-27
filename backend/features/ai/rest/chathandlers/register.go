package chathandlers

import (
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/gin-gonic/gin"
)

type ChatHandler struct {
	service chatservice.Service
}

func (ch *ChatHandler) Register(router gin.IRouter, service chatservice.Service) {
	ch.service = service

	group := router.Group("/ai/chat")

	group.POST("/complete", ch.Complete)
	group.POST("/stream", ch.Stream) // Server-Sent Events
}
