package extensionshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/extensions/services/extensionsservice"
	"github.com/gin-gonic/gin"
)

type ExtensionsHandler struct {
	service extensionsservice.Service
}

func (eh *ExtensionsHandler) Register(router gin.IRouter, service extensionsservice.Service) {
	eh.service = service

	group := router.Group("/extensions")

	group.GET("/points", eh.ListPoints)
}
