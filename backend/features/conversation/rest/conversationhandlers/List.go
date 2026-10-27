package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// List handles POST /api/v1/conversation/list.
func (ch *ConversationHandler) List(c *gin.Context) {
	var req conversationdto.ListRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.List(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), conversationdto.ListResponseFrom(resp))
}
