package bootstraphandlers

import (
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
	"github.com/gin-gonic/gin"
)

type BootstrapHandler struct {
	service bootstrapservice.Service
}

// Register maps the bootstrap routes, which need no session: on the sealed
// router, and on the public group once the server is ready.
func (bh *BootstrapHandler) Register(router gin.IRouter, service bootstrapservice.Service) {
	bh.service = service

	group := router.Group("/bootstrap")

	group.GET("/status", bh.Status)
	group.POST("/unseal", bh.Unseal)
}
