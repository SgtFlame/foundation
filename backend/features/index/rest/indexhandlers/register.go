package indexhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/gin-gonic/gin"
)

type IndexHandler struct {
	service indexservice.Service
}

func (ih *IndexHandler) Register(router gin.IRouter, service indexservice.Service) {
	ih.service = service

	group := router.Group("/index")

	group.POST("/find", ih.Find)
	group.POST("/files/index", ih.IndexFile)
	group.POST("/reindex", ih.Reindex)
}
