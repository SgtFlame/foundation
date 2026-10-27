package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Feedback handles POST /api/v1/conversation/feedback.
func (ch *ConversationHandler) Feedback(c *gin.Context) {
	var req conversationdto.FeedbackRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Feedback(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), conversationdto.FeedbackResponseFrom(resp))
}
