package embedhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/ai/rest/embeddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Embed handles POST /api/v1/ai/embed.
func (eh *EmbedHandler) Embed(c *gin.Context) {
	var req embeddto.EmbedRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := eh.service.Embed(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), embeddto.EmbedResponseFrom(resp))
}
