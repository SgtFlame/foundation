package identityhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"github.com/gin-gonic/gin"
)

type IdentityHandler struct {
	service identityservice.Service
}

// Register maps the identity routes. They work signed out, so serve.go
// registers them outside rest.RequireUser; the operations check the
// principal themselves.
func (ih *IdentityHandler) Register(router gin.IRouter, service identityservice.Service) {
	ih.service = service

	group := router.Group("/identity")

	group.GET("/status", ih.Status)
	group.POST("/setup", ih.Setup)
	group.POST("/login", ih.Login)
	group.POST("/refresh", ih.Refresh)
	group.POST("/logout", ih.Logout)
	group.POST("/switch-tenant", ih.SwitchTenant)
	group.POST("/account", ih.UpdateAccount)
}
