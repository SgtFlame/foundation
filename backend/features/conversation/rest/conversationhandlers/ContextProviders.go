package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ContextProviders handles GET /api/v1/conversation/context-providers; it takes no body.
func (ch *ConversationHandler) ContextProviders(c *gin.Context) {
	resp := ch.service.ContextProviders(conversationservice.ContextProvidersArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), conversationdto.ContextProvidersResponseFrom(resp))
}
