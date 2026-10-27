package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Answer handles POST /api/v1/conversation/answer.
func (ch *ConversationHandler) Answer(c *gin.Context) {
	var req conversationdto.AnswerRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Answer(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), conversationdto.AnswerResponseFrom(resp))
}
