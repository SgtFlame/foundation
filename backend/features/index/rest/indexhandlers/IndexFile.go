package indexhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/index/rest/indexdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// IndexFile handles POST /api/v1/index/files/index.
func (ih *IndexHandler) IndexFile(c *gin.Context) {
	var req indexdto.IndexFileRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ih.service.IndexFile(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), indexdto.IndexFileResponseFrom(resp))
}
