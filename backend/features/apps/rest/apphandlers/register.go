package apphandlers

import (
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/gin-gonic/gin"
)

type AppHandler struct {
	service appservice.Service
}

func (ah *AppHandler) Register(router gin.IRouter, service appservice.Service) {
	ah.service = service

	group := router.Group("/apps")

	group.POST("/export", ah.Export)
	group.POST("/check", ah.Check)
	group.POST("/import", ah.Import)
}
