package collabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/gin-gonic/gin"
)

type CollabHandler struct {
	service collabservice.Service
}

func (ch *CollabHandler) Register(router gin.IRouter, service collabservice.Service) {
	ch.service = service

	group := router.Group("/kb/collab")

	group.POST("/files/open", ch.OpenFile)
}

// RegisterDocs registers POST /collab/docs/open, which every tenant may
// use: a document of a contributed kind (a record's markdown field, forge
// 07l) is not a Volume.
func (ch *CollabHandler) RegisterDocs(router gin.IRouter, service collabservice.Service) {
	ch.service = service
	router.POST("/collab/docs/open", ch.OpenDoc)
}
