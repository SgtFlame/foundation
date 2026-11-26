package livehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	"github.com/gin-gonic/gin"
)

type LiveHandler struct {
	service liveservice.Service
}

func (lh *LiveHandler) Register(router gin.IRouter, service liveservice.Service) {
	lh.service = service

	router.GET("/live", lh.Serve) // WebSocket
}
