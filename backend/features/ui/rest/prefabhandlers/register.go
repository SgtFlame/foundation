package prefabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
	"github.com/gin-gonic/gin"
)

type PrefabHandler struct {
	service prefabservice.Service
}

func (ph *PrefabHandler) Register(router gin.IRouter, service prefabservice.Service) {
	ph.service = service

	group := router.Group("/ui/prefabs")

	group.GET("", ph.ListPrefabs)
	group.POST("/get", ph.GetPrefab)
	group.POST("/versions", ph.ListVersions)
	group.POST("/preview", ph.Preview)
}
