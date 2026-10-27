package indexhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/index/rest/indexdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Reindex handles POST /api/v1/index/reindex.
func (ih *IndexHandler) Reindex(c *gin.Context) {
	var req indexdto.ReindexRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ih.service.Reindex(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), indexdto.ReindexResponseFrom(resp))
}
