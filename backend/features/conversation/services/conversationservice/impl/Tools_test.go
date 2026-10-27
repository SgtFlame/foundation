package impl

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// scriptedChat streams the next message of its script for each chat call,
// and titles conversations like fakeChat.
type scriptedChat struct {
	mu     sync.Mutex
	script []chatservice.Message
	got    []chatservice.CompleteArgs
}

func (c *scriptedChat) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	return chatservice.CompleteResponse{Common: features.OK(), Message: chatservice.Message{Role: chatservice.RoleAssistant, Content: "A title"}}
}

func (c *scriptedChat) Stream(args chatservice.StreamArgs) *chatservice.StreamRequest {
	c.mu.Lock()
	c.got = append(c.got, args)
	msg := chatservice.Message{Role: chatservice.RoleAssistant, Content: "end"}
	if len(c.script) > 0 {
		msg, c.script = c.script[0], c.script[1:]
	}
	c.mu.Unlock()
	deltas := make(chan chatservice.Delta, 1)
	if msg.Content != "" {
		deltas <- chatservice.Delta{Text: msg.Content}
	}
	close(deltas)
	resp := make(chan *chatservice.CompleteResponse, 1)
	resp <- &chatservice.CompleteResponse{Common: features.OK(), Message: msg, Model: "m-flash", FinishReason: "stop", Usage: chatservice.Usage{InputTokens: 1000}}
	return &chatservice.StreamRequest{Deltas: deltas, Response: resp}
}

func TestSendRunsToolsWithApprovalAndClientCalls(t *testing.T) {
	chat := &scriptedChat{script: []chatservice.Message{
		{Role: chatservice.RoleAssistant, Content: "Searching.", ToolCalls: []chatservice.ToolCall{{Name: "test_find", Args: json.RawMessage(`{}`)}}},
		{Role: chatservice.RoleAssistant, ToolCalls: []chatservice.ToolCall{{Name: "chat_askChoice", Args: json.RawMessage(`{"question":"Which?"}`)}}},
		{Role: chatservice.RoleAssistant, ToolCalls: []chatservice.ToolCall{{Name: "test_write", Args: json.RawMessage(`{}`)}}},
		{Role: chatservice.RoleAssistant, Content: "Done [2]."},
	}}
	s, _ := newTestService(t, &fakeChat{})
	s.chat = chat
	wrote := 0
	tools.Tools.Register(s.registry, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "test.find"}, Effect: tools.EffectRead,
		Run: func(tools.Call) (tools.Result, error) {
			return tools.Result{Summary: "found", Segments: []retrieval.Scored{{Segment: retrieval.Segment{SourceID: "library", Key: "k9", Title: "notes", Text: "x"}}}}, nil
		},
	})
	tools.Tools.Register(s.registry, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "test.write"}, Effect: tools.EffectWrite,
		Run: func(tools.Call) (tools.Result, error) { wrote++; return tools.Result{Summary: "wrote"}, nil },
	})
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	req := s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "go",
		ClientTools: []tools.Tool{{Descriptor: extensions.Descriptor{ID: "chat.askChoice"}, Effect: tools.EffectUI, Location: tools.LocationClient, Wait: tools.WaitForUser}}})

	var roundID uint64
	var statuses []string
	var sources [][]retrieval.RoundSegment
	var done *conversationservice.SendEvent
	for e := range req.Events {
		switch e.Kind {
		case conversationservice.EventStarted:
			roundID = e.Round.ID
		case conversationservice.EventSources:
			sources = append(sources, e.Segments)
		case conversationservice.EventTool:
			tc := e.ToolCall
			statuses = append(statuses, tc.Name+":"+string(tc.Status))
			answer := conversationservice.AnswerArgs{ServiceArgs: args, RoundID: roundID, CallID: tc.CallID}
			switch tc.Status {
			case tools.StatusClient:
				answer.Result = json.RawMessage(`{"choice":"B"}`)
			case tools.StatusAwaiting:
				answer.Approval = tools.ApprovalAlways
				// Another user cannot answer it
				other := answer
				other.Principal.UserID = 99
				if resp := s.Answer(other); resp.Common.Code != http.StatusNotFound {
					t.Errorf("another user's answer: %+v", resp.Common)
				}
			default:
				continue
			}
			if resp := s.Answer(answer); resp.Common.Code != http.StatusOK {
				t.Fatalf("Answer: %+v", resp.Common)
			}
		case conversationservice.EventDone:
			done = &e
		}
	}

	if done == nil || done.Round.Status != http.StatusOK || done.Round.Reply != "Searching.\n\nDone [2]." || done.Round.InputTokens != 4000 {
		t.Fatalf("done = %+v", done)
	}
	want := "test.find:running test.find:done chat.askChoice:client chat.askChoice:done test.write:awaiting test.write:running test.write:done"
	if got := strings.Join(statuses, " "); got != want || wrote != 1 {
		t.Fatalf("statuses %s, wrote %d", got, wrote)
	}
	// The search's segment is [1]: the test provider's item is not citable
	if len(sources) != 1 || sources[0][0].Number != 1 || sources[0][0].SegmentKey != "k9" {
		t.Fatalf("sources = %+v", sources)
	}
	if !strings.Contains(chat.got[0].System, "# Tools") || len(chat.got[0].Tools) != 3 || !strings.Contains(string(chat.got[2].Messages[len(chat.got[2].Messages)-1].Results[0].Result), `"choice":"B"`) {
		t.Fatalf("first call %+v", chat.got[0])
	}

	rounds := s.Rounds(conversationservice.RoundsArgs{ServiceArgs: args, ConversationID: done.Round.ConversationID})
	if len(rounds.ToolCalls) != 3 || rounds.ToolCalls[2].Approval != tools.ApprovalAlways || rounds.ToolCalls[0].Cited != "[1]" {
		t.Fatalf("stored calls = %+v", rounds.ToolCalls)
	}
	conv, _ := s.find(args, done.Round.ConversationID)
	if conv.AllowedTools != `["test.write"]` {
		t.Fatalf("allowed tools = %q", conv.AllowedTools)
	}

	// With the tools setting off, nothing is offered
	collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: conv.ID, Text: "again",
		ContextSettings: map[string]json.RawMessage{tools.ContextID: json.RawMessage(`{"enabled": false}`)}}))
	if last := chat.got[len(chat.got)-1]; len(last.Tools) != 0 || strings.Contains(last.System, "# Tools") {
		t.Fatalf("tools offered while off: %+v", last.Tools)
	}
}

func TestSendRejectsWritingClientTools(t *testing.T) {
	s, _ := newTestService(t, &fakeChat{})
	events := collect(s.Send(conversationservice.SendArgs{ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()}, Text: "x",
		ClientTools: []tools.Tool{{Descriptor: extensions.Descriptor{ID: "bad"}, Effect: tools.EffectWrite, Location: tools.LocationClient}}}))
	if f := events[conversationservice.EventFailed]; len(f) != 1 || f[0].Common.Code != http.StatusBadRequest {
		t.Fatalf("events = %+v", events)
	}
}
