package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/gin-gonic/gin"
)

type ChronicleHandler struct {
	service chronicleservice.Service
}

func (ch *ChronicleHandler) Register(router gin.IRouter, service chronicleservice.Service) {
	ch.service = service

	group := router.Group("/chronicle")

	group.POST("/list", ch.List)
	group.POST("/get", ch.Get)
	group.POST("/create", ch.Create)
	group.POST("/update", ch.Update)
	group.POST("/delete", ch.Delete)
	group.POST("/link", ch.Link)
	group.GET("/types", ch.Types)
	group.POST("/types/save", ch.SaveType)
	group.POST("/types/delete", ch.DeleteType)
	group.POST("/types/copy-starter", ch.CopyStarter)
	group.POST("/reindex", ch.Reindex)
	group.POST("/share", ch.Share)
	group.POST("/shares", ch.Shares)
	group.POST("/moderate", ch.Moderate)
}
