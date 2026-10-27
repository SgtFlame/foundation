package confighandlers

import (
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/gin-gonic/gin"
)

type ConfigHandler struct {
	service configservice.Service
}

func (ch *ConfigHandler) Register(router gin.IRouter, service configservice.Service) {
	ch.service = service

	group := router.Group("/config")

	group.GET("/sections", ch.Sections)
	group.POST("/get", ch.Get)
	group.POST("/set", ch.Set)
	group.POST("/reset", ch.Reset)
	group.GET("/events", ch.Events)
}
