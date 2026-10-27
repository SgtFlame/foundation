package chroniclehandlers

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chronicledto"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// List handles POST /api/v1/chronicle/list; it takes an empty body.
func (ch *ChronicleHandler) List(c *gin.Context) {
	resp := ch.service.List(chronicleservice.ListArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), chronicledto.ListResponseFrom(resp))
}
