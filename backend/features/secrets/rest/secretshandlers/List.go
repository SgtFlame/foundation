package secretshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/secretsdto"
	"github.com/gin-gonic/gin"
)

// List handles POST /api/v1/secrets/list.
func (sh *SecretsHandler) List(c *gin.Context) {
	var req secretsdto.ListRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := sh.service.List(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), secretsdto.ListResponseFrom(resp))
}
