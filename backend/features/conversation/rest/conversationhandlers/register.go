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
	group.POST("/feedback", ch.Feedback)
	group.POST("/answer", ch.Answer)         // a waiting tool call's approval or client result
	group.POST("/mode", ch.Mode)             // switches a conversation's mode (forge 15e)
	group.POST("/workspaces", ch.Workspaces) // Move to… and Show in… (forge 16d)
	group.GET("/context-providers", ch.ContextProviders)
	group.GET("/modes", ch.Modes)
}
