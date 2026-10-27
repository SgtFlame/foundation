package indexhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/index/rest/indexdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Find handles POST /api/v1/index/find.
func (ih *IndexHandler) Find(c *gin.Context) {
	var req indexdto.FindRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ih.service.Find(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), indexdto.FindResponseFrom(resp))
}
