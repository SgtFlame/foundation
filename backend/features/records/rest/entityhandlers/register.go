package entityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
	"github.com/gin-gonic/gin"
)

type EntityHandler struct {
	service entityservice.Service
}

func (eh *EntityHandler) Register(router gin.IRouter, service entityservice.Service) {
	eh.service = service

	group := router.Group("/records/entities")

	group.GET("", eh.ListEntityTypes)
	group.POST("/get", eh.GetEntityType)
	group.POST("/versions", eh.ListVersions)
}
