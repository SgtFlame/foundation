package recordhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/gin-gonic/gin"
)

type RecordHandler struct {
	service recordservice.Service
}

func (rh *RecordHandler) Register(router gin.IRouter, service recordservice.Service) {
	rh.service = service

	group := router.Group("/records/records")

	group.POST("/get", rh.GetRecord)
	group.POST("/list", rh.ListRecords)
	group.POST("/find", rh.FindRecords)
	group.POST("/aggregate", rh.AggregateRecords)
	group.POST("/import", rh.ImportRecords)
	group.POST("/search", rh.SearchRecords)

	files := router.Group("/records/files")
	files.POST("/upload", rh.UploadFile)
	files.GET("/:entity/:record/:field/:file", rh.OpenFile)
}
