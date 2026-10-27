package secretshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
	"github.com/gin-gonic/gin"
)

type SecretsHandler struct {
	service secretsservice.Service
}

func (sh *SecretsHandler) Register(router gin.IRouter, service secretsservice.Service) {
	sh.service = service

	group := router.Group("/secrets")

	group.GET("/status", sh.Status)
	group.POST("/list", sh.List)
	group.POST("/put", sh.Put)
	group.POST("/delete", sh.Delete)
}
