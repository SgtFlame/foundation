package tenanthandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"github.com/gin-gonic/gin"
)

type TenantHandler struct {
	service tenantservice.Service
}

func (th *TenantHandler) Register(router gin.IRouter, service tenantservice.Service) {
	th.service = service

	group := router.Group("/identity/tenants")

	group.POST("/list", th.List)
	group.POST("/create", th.Create)
	group.POST("/rename", th.Rename)
	group.POST("/delete", th.Delete)
	group.POST("/members", th.Members)
	group.POST("/add-member", th.AddMember)
	group.POST("/remove-member", th.RemoveMember)
}
