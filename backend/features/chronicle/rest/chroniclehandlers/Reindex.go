package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Reindex handles POST /api/v1/chronicle/reindex.
func (ch *ChronicleHandler) Reindex(c *gin.Context) {
	var req chronicledto.ReindexRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Reindex(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chronicledto.ReindexResponseFrom(resp))
}
