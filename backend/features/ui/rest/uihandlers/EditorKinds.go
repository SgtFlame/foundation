package uihandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/uidto"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
	"github.com/gin-gonic/gin"
)

// EditorKinds handles GET /api/v1/ui/editor-kinds.
func (uh *UiHandler) EditorKinds(c *gin.Context) {
	resp := uh.service.EditorKinds(uiservice.EditorKindsArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), uidto.EditorKindsResponseFrom(resp))
}
