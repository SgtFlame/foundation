package chathandlers

import (
	"io"

	"github.com/SgtFlame/foundation/backend/features/ai/rest/chatdto"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Stream handles POST /api/v1/ai/chat/stream, answering with Server-Sent
// Events: "delta" events as the reply is generated, then "done" with the
// complete response, or "error". A request that fails before generation starts
// gets a plain JSON error with its status instead.
func (ch *ChatHandler) Stream(c *gin.Context) {
	var req chatdto.StreamRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	stream := ch.service.Stream(req.ToArgs(args))

	// Wait for the first piece, so a request that fails up front gets a status
	first, more := <-stream.Deltas
	var resp *chatservice.CompleteResponse
	if !more {
		resp = <-stream.Response
		if resp.Common.Code >= 300 {
			c.JSON(int(resp.Common.Code), chatdto.CompleteResponseFrom(*resp))
			return
		}
	}

	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	if more {
		c.SSEvent("delta", chatdto.StreamDelta{Text: first.Text})
	}
	c.Stream(func(w io.Writer) bool {
		if resp == nil {
			if delta, ok := <-stream.Deltas; ok {
				c.SSEvent("delta", chatdto.StreamDelta{Text: delta.Text})
				return true
			}
			resp = <-stream.Response
		}
		if resp.Common.Code >= 300 {
			c.SSEvent("error", chatdto.StreamError{Code: resp.Common.Code, Message: resp.Common.Message, ErrorCode: resp.Common.ErrorCode})
		} else {
			c.SSEvent("done", chatdto.CompleteResponseFrom(*resp))
		}
		return false
	})
}
