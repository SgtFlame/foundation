package cognitionhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/gin-gonic/gin"
)

type CognitionHandler struct {
	service cognitionservice.Service
}

func (ch *CognitionHandler) Register(router gin.IRouter, service cognitionservice.Service) {
	ch.service = service

	group := router.Group("/cognition")

	group.GET("/graphs", ch.ListGraphs)
	group.POST("/graph", ch.GetGraph)
	group.GET("/node-types", ch.NodeTypes)
	group.POST("/runs", ch.ListRuns)
	group.POST("/trace", ch.GetTrace)
}
