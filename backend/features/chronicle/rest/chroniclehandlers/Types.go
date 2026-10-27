package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Types handles GET /api/v1/chronicle/types; it takes no body.
func (ch *ChronicleHandler) Types(c *gin.Context) {
	resp := ch.service.Types(chronicleservice.TypesArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), chronicledto.TypesResponseFrom(resp))
}
