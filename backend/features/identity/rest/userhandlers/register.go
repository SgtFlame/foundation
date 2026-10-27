package userhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	service userservice.Service
}

func (uh *UserHandler) Register(router gin.IRouter, service userservice.Service) {
	uh.service = service

	group := router.Group("/identity/users")

	group.POST("/list", uh.List)
	group.POST("/create", uh.Create)
	group.POST("/update", uh.Update)
}
