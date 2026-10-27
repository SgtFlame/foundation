package retrievalhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/retrieval/rest/retrievaldto"
	"github.com/gin-gonic/gin"
)

// Retrieve handles POST /api/v1/retrieval/retrieve.
func (rh *RetrievalHandler) Retrieve(c *gin.Context) {
	var req retrievaldto.RetrieveRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := rh.service.Retrieve(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), retrievaldto.RetrieveResponseFrom(resp))
}
