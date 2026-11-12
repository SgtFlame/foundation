package conversationhandlers

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/rest/chatdto"
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationdto"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// SendCall is the live call that sends a message and streams its round.
const SendCall = "conversation.send"

// ContributeLive registers the conversation.send call, given the service
// once made. Its events are "started", "sources" (the context the round may
// cite, sent again when a tool adds citations), "delta" as the reply is
// generated, "tool" as each tool call changes status, "done" with the
// completed round, "title" when the conversation is named, and "followed"
// for what each round follower did. A message refused before its round is
// stored ends the call with its status; a reply that fails afterwards is a
// "done" round with its status. Cancelling the call stops the reply. It
// replaces POST /conversation/send (01-ServerUI question 9).
func ContributeLive(reg *extensions.Registry, service func() conversationservice.Service) {
	live.Calls.Register(reg, live.Call{
		Descriptor:  extensions.Descriptor{ID: SendCall, Source: "chat"},
		Title:       "Send a chat message",
		Description: "Sends a message to a conversation and streams its round.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var req conversationdto.SendRequest
			if fail := rest.BindJSON(c.ServiceArgs, c.Args, &req); fail != nil {
				return fail
			}
			for e := range service().Send(req.ToArgs(c.ServiceArgs)).Events {
				if e.Kind == conversationservice.EventFailed && e.Round == nil {
					common := e.Common
					return &common
				}
				send(c, e)
			}
			return nil
		},
	})
}

func send(c live.CallRun, e conversationservice.SendEvent) {
	switch e.Kind {
	case conversationservice.EventStarted:
		c.Emit("started", conversationdto.SendStarted{Conversation: conversationdto.ConversationFrom(*e.Conversation), Round: conversationdto.RoundFrom(*e.Round)})
	case conversationservice.EventSources:
		out := conversationdto.SendSources{}
		for _, seg := range e.Segments {
			out.Segments = append(out.Segments, conversationdto.RoundSegmentFrom(seg))
		}
		c.Emit("sources", out)
	case conversationservice.EventDelta:
		c.Emit("delta", conversationdto.SendDelta{Text: e.Text})
	case conversationservice.EventDone:
		c.Emit("done", conversationdto.SendDone{Round: conversationdto.RoundFrom(*e.Round)})
	case conversationservice.EventTitle:
		c.Emit("title", conversationdto.SendTitle{Conversation: conversationdto.ConversationFrom(*e.Conversation)})
	case conversationservice.EventTool:
		c.Emit("tool", conversationdto.ToolCallFrom(*e.ToolCall))
	case conversationservice.EventFollowed:
		c.Emit("followed", conversationdto.SendFollowed{Follower: e.Follower, Summary: e.Followed.Summary, Data: e.Followed.Data})
	case conversationservice.EventRetry:
		c.Emit("retry", conversationdto.SendRetry{Try: e.Retry.Try, WaitMs: e.Retry.WaitMs, Status: e.Retry.Status})
	case conversationservice.EventFailed:
		c.Emit("error", chatdto.StreamError{Code: e.Common.Code, Message: e.Common.Message, ErrorCode: e.Common.ErrorCode})
	}
}
