package embedhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/gin-gonic/gin"
)

type EmbedHandler struct {
	service embedservice.Service
}

func (eh *EmbedHandler) Register(router gin.IRouter, service embedservice.Service) {
	eh.service = service

	group := router.Group("/ai/embed")

	group.POST("", eh.Embed)
	group.POST("/describe", eh.Describe)
}
