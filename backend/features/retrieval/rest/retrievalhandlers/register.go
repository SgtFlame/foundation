package retrievalhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/gin-gonic/gin"
)

type RetrievalHandler struct {
	service retrievalservice.Service
}

func (rh *RetrievalHandler) Register(router gin.IRouter, service retrievalservice.Service) {
	rh.service = service

	group := router.Group("/retrieval")

	group.POST("/retrieve", rh.Retrieve)
}
