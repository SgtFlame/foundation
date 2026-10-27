package secretshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/secretsdto"
	"github.com/gin-gonic/gin"
)

// Delete handles POST /api/v1/secrets/delete.
func (sh *SecretsHandler) Delete(c *gin.Context) {
	var req secretsdto.DeleteRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := sh.service.Delete(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), secretsdto.DeleteResponseFrom(resp))
}
