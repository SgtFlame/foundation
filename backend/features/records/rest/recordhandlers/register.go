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
	group.POST("/import", rh.ImportRecords)
}
