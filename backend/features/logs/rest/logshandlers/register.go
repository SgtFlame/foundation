package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/gin-gonic/gin"
)

type LogsHandler struct {
	service logsservice.Service
}

func (lh *LogsHandler) Register(router gin.IRouter, service logsservice.Service) {
	lh.service = service

	group := router.Group("/logs")

	group.GET("/status", lh.Status)
	group.POST("/query", lh.Query)
	group.POST("/hits", lh.Hits)
	group.POST("/fields/names", lh.FieldNames)
	group.POST("/fields/values", lh.FieldValues)
	group.POST("/streams", lh.Streams)
	group.POST("/tail", lh.Tail)
	group.POST("/ingest", lh.Ingest)
	group.POST("/user-level", lh.SetUserLevel)
	group.POST("/user-levels", lh.UserLevels)
	group.GET("/level", lh.Level)
}
