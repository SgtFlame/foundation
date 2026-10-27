package authzhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	"github.com/gin-gonic/gin"
)

type AuthzHandler struct {
	service authzservice.Service
}

func (ah *AuthzHandler) Register(router gin.IRouter, service authzservice.Service) {
	ah.service = service

	group := router.Group("/authz")

	group.GET("/me", ah.Me)
	group.GET("/resource-types", ah.ResourceTypes)
	group.GET("/roles", ah.Roles)
	group.POST("/roles/save", ah.SaveRole)
	group.POST("/roles/delete", ah.DeleteRole)
	group.POST("/grants", ah.Grants)
	group.POST("/grant", ah.Grant)
	group.POST("/revoke", ah.Revoke)
	group.POST("/explain", ah.Explain)
}
