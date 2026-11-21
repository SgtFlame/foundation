package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Modes handles GET /api/v1/conversation/modes; it takes no body.
func (ch *ConversationHandler) Modes(c *gin.Context) {
	resp := ch.service.Modes(conversationservice.ModesArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), conversationdto.ModesResponseFrom(resp))
}
