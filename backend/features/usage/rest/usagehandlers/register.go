package usagehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
	"github.com/gin-gonic/gin"
)

type UsageHandler struct {
	service usageservice.Service
}

func (uh *UsageHandler) Register(router gin.IRouter, service usageservice.Service) {
	uh.service = service

	group := router.Group("/usage")

	group.POST("/summary", uh.Summary)
	group.POST("/records", uh.Records)
	group.POST("/capture", uh.Capture)
}
