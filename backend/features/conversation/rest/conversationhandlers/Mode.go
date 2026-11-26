package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Mode handles POST /api/v1/conversation/mode (forge 15e).
func (ch *ConversationHandler) Mode(c *gin.Context) {
	var req conversationdto.ModeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.SetMode(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), conversationdto.ModeResponseFrom(resp))
}
