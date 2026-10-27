package secretshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/secretsdto"
	"github.com/gin-gonic/gin"
)

// Put handles POST /api/v1/secrets/put.
func (sh *SecretsHandler) Put(c *gin.Context) {
	var req secretsdto.PutRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := sh.service.Put(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), secretsdto.PutResponseFrom(resp))
}
