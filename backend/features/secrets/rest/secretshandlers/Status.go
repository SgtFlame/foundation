package secretshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/secretsdto"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
	"github.com/gin-gonic/gin"
)

// Status handles GET /api/v1/secrets/status.
func (sh *SecretsHandler) Status(c *gin.Context) {
	resp := sh.service.Status(secretsservice.StatusArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), secretsdto.StatusResponseFrom(resp))
}
