package volumehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/gin-gonic/gin"
)

type VolumeHandler struct {
	service volumeservice.Service
}

func (vh *VolumeHandler) Register(router gin.IRouter, service volumeservice.Service) {
	vh.service = service

	group := router.Group("/kb/volumes")

	group.POST("/folders/list", vh.ListFolder)
	group.POST("/folders/make", vh.MakeFolder)
	group.POST("/folders/remove", vh.RemoveFolder)
	group.POST("/create", vh.CreateVolume)
	group.POST("/files/create", vh.CreateFile)
}
