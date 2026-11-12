package uihandlers

import (
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
	"github.com/gin-gonic/gin"
)

type UiHandler struct {
	service uiservice.Service
}

func (uh *UiHandler) Register(router gin.IRouter, service uiservice.Service) {
	uh.service = service

	group := router.Group("/ui")

	group.GET("/components", uh.Components)
	group.GET("/editor-kinds", uh.EditorKinds)
}
