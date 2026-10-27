package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Rounds handles POST /api/v1/conversation/rounds.
func (ch *ConversationHandler) Rounds(c *gin.Context) {
	var req conversationdto.RoundsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Rounds(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), conversationdto.RoundsResponseFrom(resp))
}
