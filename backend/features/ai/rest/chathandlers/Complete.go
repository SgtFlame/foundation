package chathandlers

import (
	"github.com/SgtFlame/foundation/backend/features/ai/rest/chatdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Complete handles POST /api/v1/ai/chat/complete.
func (ch *ChatHandler) Complete(c *gin.Context) {
	var req chatdto.CompleteRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.Complete(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), chatdto.CompleteResponseFrom(resp))
}
