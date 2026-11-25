package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Workspaces handles POST /api/v1/conversation/workspaces (forge 16d).
func (ch *ConversationHandler) Workspaces(c *gin.Context) {
	var req conversationdto.WorkspacesRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := ch.service.SetWorkspaces(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), conversationdto.WorkspacesResponseFrom(resp))
}
