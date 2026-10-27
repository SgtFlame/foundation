package conversationhandlers

import (
	"io"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/ai/rest/chatdto"
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Send handles POST /api/v1/conversation/send, answering with Server-Sent
// Events: "started", "sources" (the context it may cite, sent again when a
// tool adds citations), "delta" events as the reply is generated, "tool"
// as each tool call changes status, "done" with the completed round, "title" when the
// conversation is named, and "followed" for what each round follower did. A message
// refused before its round is stored gets a plain JSON error with its status
// instead; a reply that fails afterwards is a "done" round with its status.
func (ch *ConversationHandler) Send(c *gin.Context) {
	var req conversationdto.SendRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	events := ch.service.Send(req.ToArgs(args)).Events
	first, more := <-events
	if !more {
		c.JSON(http.StatusInternalServerError, rest.Status{Message: "the reply ended before it started"})
		return
	}
	if first.Kind == conversationservice.EventFailed {
		c.JSON(int(first.Common.Code), rest.StatusFrom(first.Common))
		return
	}

	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	send(c, first)
	c.Stream(func(w io.Writer) bool {
		e, ok := <-events
		if !ok {
			return false
		}
		send(c, e)
		return true
	})
}

func send(c *gin.Context, e conversationservice.SendEvent) {
	switch e.Kind {
	case conversationservice.EventStarted:
		c.SSEvent("started", conversationdto.SendStarted{Conversation: conversationdto.ConversationFrom(*e.Conversation), Round: conversationdto.RoundFrom(*e.Round)})
	case conversationservice.EventSources:
		out := conversationdto.SendSources{}
		for _, seg := range e.Segments {
			out.Segments = append(out.Segments, conversationdto.RoundSegmentFrom(seg))
		}
		c.SSEvent("sources", out)
	case conversationservice.EventDelta:
		c.SSEvent("delta", conversationdto.SendDelta{Text: e.Text})
	case conversationservice.EventDone:
		c.SSEvent("done", conversationdto.SendDone{Round: conversationdto.RoundFrom(*e.Round)})
	case conversationservice.EventTitle:
		c.SSEvent("title", conversationdto.SendTitle{Conversation: conversationdto.ConversationFrom(*e.Conversation)})
	case conversationservice.EventTool:
		c.SSEvent("tool", conversationdto.ToolCallFrom(*e.ToolCall))
	case conversationservice.EventFollowed:
		c.SSEvent("followed", conversationdto.SendFollowed{Follower: e.Follower, Summary: e.Followed.Summary, Data: e.Followed.Data})
	case conversationservice.EventFailed:
		c.SSEvent("error", chatdto.StreamError{Code: e.Common.Code, Message: e.Common.Message, ErrorCode: e.Common.ErrorCode})
	}
}
