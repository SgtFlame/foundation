package collabhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/kb/rest/collabdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// OpenFile handles POST /api/v1/kb/collab/files/open.
func (ch *CollabHandler) OpenFile(c *gin.Context) {
	var req collabdto.OpenFileRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.OpenFile(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), collabdto.OpenFileResponseFrom(resp))
}
