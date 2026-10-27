package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/gin-gonic/gin"
)

type ConversationHandler struct {
	service conversationservice.Service
}

func (ch *ConversationHandler) Register(router gin.IRouter, service conversationservice.Service) {
	ch.service = service

	group := router.Group("/conversation")

	group.POST("/list", ch.List)
	group.POST("/rounds", ch.Rounds)
	group.POST("/send", ch.Send) // Server-Sent Events
	group.POST("/feedback", ch.Feedback)
	group.POST("/answer", ch.Answer) // a waiting tool call's approval or client result
	group.GET("/context-providers", ch.ContextProviders)
}
