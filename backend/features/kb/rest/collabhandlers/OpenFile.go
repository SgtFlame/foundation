package collabhandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/rest/collabdto"
	"github.com/gin-gonic/gin"
)

// OpenFile handles POST /api/v1/kb/collab/files/open.
func (ch *CollabHandler) OpenFile(c *gin.Context) {
	var req collabdto.OpenFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, collabdto.OpenFileResponse{Message: err.Error()})
		return
	}

	resp := ch.service.OpenFile(req.ToArgs())
	c.JSON(int(resp.Common.Code), collabdto.OpenFileResponseFrom(resp))
}
