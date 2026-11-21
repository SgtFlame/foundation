package chathandlers

import (

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/rest/chatdto"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// StreamCall is the live call that streams a completion.
const StreamCall = "ai.chat.stream"

// ContributeLive registers the ai.chat.stream call, given the service once
// made: "delta" events as the reply is generated, then "done" with the
// complete response; a failure ends the call with its status. It replaces
// POST /ai/chat/stream (01-ServerUI question 9).
func ContributeLive(reg *extensions.Registry, service func() chatservice.Service) {
	live.Calls.Register(reg, live.Call{
		Descriptor:  extensions.Descriptor{ID: StreamCall, Source: "ai"},
		Title:       "Stream a completion",
		Description: "Streams one chat completion: delta events, then done with the complete response.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var req chatdto.StreamRequest
			if fail := rest.BindJSON(c.ServiceArgs, c.Args, &req); fail != nil {
				return fail
			}
			stream := service().Stream(req.ToArgs(c.ServiceArgs))
			for delta := range stream.Deltas {
				c.Emit("delta", chatdto.StreamDelta{Text: delta.Text})
			}
			resp := <-stream.Response
			if resp.Common.Code >= 300 {
				return &resp.Common
			}
			c.Emit("done", chatdto.CompleteResponseFrom(*resp))
			return nil
		},
	})
}

