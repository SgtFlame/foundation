package serviceshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/services/services/servicesservice"
	"github.com/gin-gonic/gin"
)

type ServicesHandler struct {
	service servicesservice.Service
}

func (sh *ServicesHandler) Register(router gin.IRouter, service servicesservice.Service) {
	sh.service = service

	group := router.Group("/services")

	group.GET("/status", sh.Status)
}
