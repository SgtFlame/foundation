package impl

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	aisettings "github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	conversationsettings "github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// preChat answers the pre-mode's call (feature chat-premode) with the next
// of its answers, and everything else as fakeChat does. block makes the
// pre-mode's call wait until it is cancelled (a timeout).
type preChat struct {
	*fakeChat
	mu      sync.Mutex
	answers []string
	asked   []chatservice.CompleteArgs
	block   bool
}

func (c *preChat) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	if args.Meta.Feature != "chat-premode" {
		return c.fakeChat.Complete(args)
	}
	c.mu.Lock()
	c.asked = append(c.asked, args)
	answer := `{"decision": "stay", "mode": "chat", "confidence": 1, "reason": ""}`
	if len(c.answers) > 0 {
		answer, c.answers = c.answers[0], c.answers[1:]
	}
	block := c.block
	c.mu.Unlock()
	if block {
		<-args.Context().Done()
		return chatservice.CompleteResponse{Common: features.ServiceResponse{Code: 504, Message: "cancelled"}}
	}
	return chatservice.CompleteResponse{Common: features.OK(), Message: chatservice.Message{Role: chatservice.RoleAssistant, Content: answer}}
}

func (c *preChat) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.asked)
}

func newPreModeService(t *testing.T, chatSection string) (*service, *preChat) {
	t.Helper()
	s, _ := newTestService(t, &fakeChat{})
	chat := &preChat{fakeChat: &fakeChat{}}
	s.chat = chat
	cfg := s.config.(sections)
	if chatSection != "" {
		cfg[conversationsettings.ID] = chatSection
	}
	cfg[aisettings.StagesID] = `{"match": "fast", "memory": "fast", "premode": "fast"}`
	return s, chat
}

// send sends a message and answers the pre-mode's question with answer
// ("switch" or "stay"; empty leaves it unanswered), returning its events.
func sendAnswering(t *testing.T, s *service, a conversationservice.SendArgs, answer string) map[conversationservice.SendEventKind][]conversationservice.SendEvent {
	t.Helper()
	out := map[conversationservice.SendEventKind][]conversationservice.SendEvent{}
	for e := range s.Send(a).Events {
		out[e.Kind] = append(out[e.Kind], e)
		if e.Kind == conversationservice.EventTool && e.ToolCall.Name == "chat.askChoice" && e.ToolCall.Status == tools.StatusClient && answer != "" {
			res := s.Answer(conversationservice.AnswerArgs{ServiceArgs: a.ServiceArgs, RoundID: e.ToolCall.RoundID, CallID: e.ToolCall.CallID,
				Result: json.RawMessage(`{"choice": "x", "value": "` + answer + `"}`)})
			if res.Common.Code != 200 {
				t.Fatalf("answer %+v", res.Common)
			}
		}
	}
	return out
}

func decisionOf(t *testing.T, r *conversation.Round) conversation.PreModeDecision {
	t.Helper()
	var d conversation.PreModeDecision
	if r.PreMode == "" || json.Unmarshal([]byte(r.PreMode), &d) != nil {
		t.Fatalf("round without a pre-mode decision: %+v", r)
	}
	return d
}

func asked(events map[conversationservice.SendEventKind][]conversationservice.SendEvent) bool {
	for _, e := range events[conversationservice.EventTool] {
		if e.ToolCall.Name == "chat.askChoice" && e.ToolCall.Status == tools.StatusClient {
			return true
		}
	}
	return false
}

// forge 15f: the first switch asks; answered Switch, the round runs in the
// other mode, stored on the round and the conversation. A later confident
// switch goes ahead without asking; an unsure one asks.
func TestPreModeAsksFirstThenSwitches(t *testing.T) {
	s, chat := newPreModeService(t, "")
	chat.answers = []string{
		`{"decision": "switch", "mode": "builder", "confidence": 0.95, "reason": "This builds an app."}`,
		`{"decision": "switch", "mode": "chat", "confidence": 0.9, "reason": "A plain question."}`,
		`{"decision": "switch", "mode": "builder", "confidence": 0.5, "reason": "Maybe the builder."}`,
	}
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	offered := []string{"chat", "builder", "nothing"}

	first := sendAnswering(t, s, conversationservice.SendArgs{ServiceArgs: args, Text: "make an app", Offered: offered}, "switch")
	if !asked(first) {
		t.Fatalf("the first switch did not ask: %+v", first)
	}
	if len(first[conversationservice.EventMode]) != 1 || first[conversationservice.EventMode][0].Switch.To != "builder" || first[conversationservice.EventMode][0].Switch.From != "chat" {
		t.Fatalf("mode events %+v", first[conversationservice.EventMode])
	}
	done := first[conversationservice.EventDone][0].Round
	if done.Mode != "builder" || decisionOf(t, done).Outcome != preAskedSwitched {
		t.Errorf("first round %+v", done)
	}
	// The pre-mode was told the offered modes that exist, by purpose, at the premode level
	if a := chat.asked[0]; a.Level != "fast" || !strings.Contains(a.System, "You choose the chat mode") || !strings.Contains(a.System, "- builder:") || strings.Contains(a.System, "nothing") {
		t.Errorf("pre-mode call %+v", a)
	}
	conv, _ := s.find(args, done.ConversationID)
	if conv.Mode != "builder" {
		t.Errorf("conversation mode %q", conv.Mode)
	}
	rounds := s.Rounds(conversationservice.RoundsArgs{ServiceArgs: args, ConversationID: done.ConversationID})
	if len(rounds.ToolCalls) != 1 || rounds.ToolCalls[0].NodeID != preModeActor || rounds.ToolCalls[0].Status != tools.StatusDone {
		t.Errorf("stored question %+v", rounds.ToolCalls)
	}

	second := sendAnswering(t, s, conversationservice.SendArgs{ServiceArgs: args, ConversationID: done.ConversationID, Text: "what is 2+2", Offered: offered}, "")
	if asked(second) || len(second[conversationservice.EventMode]) != 1 {
		t.Fatalf("a confident later switch should not ask: %+v", second)
	}
	if r := second[conversationservice.EventDone][0].Round; r.Mode != "chat" || decisionOf(t, r).Outcome != preSwitched {
		t.Errorf("second round %+v", r)
	}

	third := sendAnswering(t, s, conversationservice.SendArgs{ServiceArgs: args, ConversationID: done.ConversationID, Text: "hm", Offered: offered}, "stay")
	if !asked(third) || len(third[conversationservice.EventMode]) != 0 {
		t.Fatalf("an unsure switch should ask: %+v", third)
	}
	if r := third[conversationservice.EventDone][0].Round; r.Mode != "chat" || decisionOf(t, r).Outcome != preAskedStayed {
		t.Errorf("third round %+v", r)
	}
}

// forge 15f: the pre-mode is not asked when the person chose the mode,
// only one mode is offered, it is off, or on an Undo; Undo reruns the
// message in the mode it left and drops the switched round from history.
func TestPreModeSkipsAndUndo(t *testing.T) {
	s, chat := newPreModeService(t, `{"systemPrompt": "Be helpful.", "historyRounds": 5, "titleLevel": "fast", "preMode": {"threshold": 0.5, "askFirst": false}}`)
	chat.answers = []string{`{"decision": "switch", "mode": "builder", "confidence": 0.6, "reason": "Building."}`}
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	offered := []string{"chat", "builder"}

	r := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "chosen", Mode: "chat", ModeChosen: true, Offered: offered}))
	conv := r[conversationservice.EventDone][0].Round.ConversationID
	collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: conv, Text: "one mode", Offered: []string{"chat"}}))
	if chat.calls() != 0 {
		t.Fatalf("the pre-mode ran %d times", chat.calls())
	}
	// Without askFirst and at a lower threshold, it switches at once
	switched := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: conv, Text: "build it", Offered: offered}))
	round := switched[conversationservice.EventDone][0].Round
	if chat.calls() != 1 || round.Mode != "builder" || len(switched[conversationservice.EventMode]) != 1 {
		t.Fatalf("switch %+v", switched)
	}

	undo := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: conv, Text: "build it", Mode: "chat", ModeChosen: true, Undoes: round.ID, Offered: offered}))
	again := undo[conversationservice.EventDone][0].Round
	if chat.calls() != 1 || again.Mode != "chat" || again.PreMode != "" {
		t.Fatalf("undo %+v", again)
	}
	var undone conversation.Round
	s.db.First(&undone, round.ID)
	if undone.Feedback != string(retrieval.FeedbackIgnore) {
		t.Errorf("the undone round stays in the history: %+v", undone)
	}
	if c, _ := s.find(args, conv); c.Mode != "chat" {
		t.Errorf("conversation mode %q", c.Mode)
	}

	s.config.(sections)[conversationsettings.ID] = `{"systemPrompt": "x", "titleLevel": "fast", "preMode": {"enabled": false}}`
	collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: conv, Text: "off", Offered: offered}))
	if chat.calls() != 1 {
		t.Errorf("the pre-mode ran while off")
	}
}

// forge 15f: a pre-mode slower than its budget leaves the round in its
// mode, and the round goes on.
func TestPreModeTimeoutStays(t *testing.T) {
	s, chat := newPreModeService(t, `{"systemPrompt": "x", "titleLevel": "fast", "preMode": {"budgetMs": 200}}`)
	chat.block = true
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	start := time.Now()
	r := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "hello", Offered: []string{"chat", "builder"}}))
	done := r[conversationservice.EventDone][0].Round
	if done.Status != 200 || done.Mode != "chat" || decisionOf(t, done).Outcome != preTimeout {
		t.Errorf("round %+v", done)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the round waited %s", time.Since(start))
	}
}

// forge 15f question 17: while a mode other than the default has just
// asked the person something, a confident switch away only asks.
func TestPreModeAsksWhileABuilderWaits(t *testing.T) {
	s, chat := newPreModeService(t, `{"systemPrompt": "x", "titleLevel": "fast", "historyRounds": 5, "preMode": {"askFirst": false}}`)
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	r := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "Shall I go ahead?", Mode: "builder", ModeChosen: true}))
	conv := r[conversationservice.EventDone][0].Round.ConversationID // fakeChat echoes the message: the reply ends with a question
	chat.answers = []string{`{"decision": "switch", "mode": "chat", "confidence": 0.99, "reason": "Small talk."}`}
	next := sendAnswering(t, s, conversationservice.SendArgs{ServiceArgs: args, ConversationID: conv, Text: "nice weather", Offered: []string{"chat", "builder"}}, "stay")
	if !asked(next) {
		t.Fatalf("it switched away from a waiting builder: %+v", next)
	}
	if done := next[conversationservice.EventDone][0].Round; done.Mode != "builder" {
		t.Errorf("round %+v", done)
	}
}
