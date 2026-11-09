package impl

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// testMode is a mode graph for the tests: a plain chat, as Personal is
// without Chronicle.
type testMode struct {
	id       string
	template string   // the instructions, a Go template over system (the chat section's) and tools
	include  []string // the tools selected; empty takes every tool
	maxSteps string   // an expression for Stream reply's steps; empty keeps the section's
	followed string   // a summary Emit followed memory reports after the title; empty reports none
}

// graph is the mode as a graph: every provider's context after the
// instructions, the tools the conversation allows, the reply, a title
// after the first round, then the follow-up.
func (m testMode) graph() cognition.Graph {
	g := cognition.Graph{ID: m.id, Name: m.id, Kind: cognition.KindMode, Spec: json.RawMessage(`{"settings": ["test", "tools"]}`)}
	node := func(id, typ string, config any) {
		raw, _ := json.Marshal(config)
		g.Nodes = append(g.Nodes, cognition.Node{ID: id, Type: typ, Config: raw})
	}
	edge := func(from, fromPin, to, toPin string) {
		g.Edges = append(g.Edges, cognition.Edge{From: cognition.PinRef{Node: from, Pin: fromPin}, To: cognition.PinRef{Node: to, Pin: toPin}})
	}
	node("on", "event.userMessage", nil)
	node("gather", "chat.gather", map[string]any{})
	node("toolsOn", "chat.settings", map[string]any{"provider": tools.ContextID})
	node("tools", "tools.select", map[string]any{"include": m.include})
	node("system", "data.setting", map[string]any{"section": "chat", "path": "systemPrompt", "type": "string"})
	node("instructions", "text.template", map[string]any{"template": m.template})
	node("context", "chat.addContext", map[string]any{})
	node("reply", "chat.reply", nil)
	node("untitled", "data.expression", map[string]any{"expr": `title == ""`, "type": "bool"})
	node("name", "flow.branch", nil)
	node("titleSystem", "text.template", map[string]any{"template": "Name this."})
	node("titlePrompt", "text.template", map[string]any{"template": "User: {{.text}}\n\nAssistant: {{.reply}}"})
	node("titleLevel", "data.setting", map[string]any{"section": "chat", "path": "titleLevel", "type": "string"})
	node("ask", "llm.complete", map[string]any{"feature": "chat-title"})
	node("setTitle", "chat.setTitle", nil)
	edge("on", "then", "gather", "then")
	edge("toolsOn", "enabled", "tools", "enabled")
	edge("system", "value", "instructions", "system")
	if strings.Contains(m.template, ".tools") {
		edge("tools", "tools", "instructions", "tools")
	}
	edge("gather", "then", "context", "then")
	edge("instructions", "text", "context", "system")
	edge("gather", "items", "context", "items")
	edge("context", "then", "reply", "then")
	edge("context", "system", "reply", "system")
	edge("tools", "tools", "reply", "tools")
	if m.maxSteps != "" {
		node("steps", "data.expression", map[string]any{"expr": m.maxSteps, "type": "number"})
		edge("steps", "value", "reply", "maxSteps")
	}
	edge("reply", "then", "name", "then")
	edge("on", "title", "untitled", "title")
	edge("untitled", "value", "name", "condition")
	edge("name", "true", "ask", "then")
	edge("titleSystem", "text", "ask", "system")
	edge("on", "text", "titlePrompt", "text")
	edge("reply", "reply", "titlePrompt", "reply")
	edge("titlePrompt", "text", "ask", "prompt")
	edge("titleLevel", "value", "ask", "level")
	edge("ask", "then", "setTitle", "then")
	edge("ask", "text", "setTitle", "title")
	if m.followed != "" {
		node("summary", "text.template", map[string]any{"template": m.followed})
		node("followed", "chat.followed", map[string]any{"follower": "memory"})
		edge("setTitle", "then", "followed", "then")
		edge("name", "false", "followed", "then")
		edge("summary", "text", "followed", "summary")
	}
	return g
}

// The modes every test service has, by chat.modes id; chat is the default.
var testModes = []testMode{
	{id: "chat", template: "{{.system}}{{if .tools}}\n\n# Tools{{end}}"},
	{id: "builder", template: "{{.system}}\n\n# Mode: Builder\nFollow the procedure.", include: []string{"test."}, maxSteps: "2"},
	{id: "memo", template: "{{.system}}", followed: "Memory: updated *e1*"},
}

const testModesSection = `{"modes": [
	{"id": "chat", "label": "Chat", "graphRef": "chat", "default": true},
	{"id": "builder", "label": "Builder", "icon": "🛠", "graphRef": "builder", "focus": {"kinds": ["prefab"]}},
	{"id": "memo", "label": "Memo", "graphRef": "memo"},
	{"id": "gone", "label": "Gone", "graphRef": "nowhere"}]}`

func contributeModes(t *testing.T, reg *extensions.Registry) {
	for i, m := range testModes {
		cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: m.id, Order: i}, Graph: m.graph()})
	}
}

// forge 06i: a mode's graph builds the prompt, narrows the tools and sets
// the steps; switching modes mid-conversation changes the next round, not
// the history; the round ends through the chat.roundEnd hooks.
func TestSendRunsTheConversationsModeGraph(t *testing.T) {
	find := chatservice.Message{Role: chatservice.RoleAssistant, ToolCalls: []chatservice.ToolCall{{Name: "test_find", Args: json.RawMessage(`{}`)}}}
	chat := &scriptedChat{script: []chatservice.Message{{Role: chatservice.RoleAssistant, Content: "hello"}, find, find, {Role: chatservice.RoleAssistant, Content: "built"}}}
	s, _ := newTestService(t, &fakeChat{})
	s.chat = chat
	tools.Tools.Register(s.registry, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "test.find"}, Effect: tools.EffectRead,
		Run: func(tools.Call) (tools.Result, error) { return tools.Result{Summary: "found"}, nil },
	})
	tools.Tools.Register(s.registry, tools.Tool{Descriptor: extensions.Descriptor{ID: "other.read"}, Effect: tools.EffectRead})
	var ended []uint64
	conversation.RoundEnds.Register(s.registry, conversation.RoundEnd{
		Descriptor: extensions.Descriptor{ID: "test"},
		End:        func(_ features.ServiceArgs, r conversation.Round) { ended = append(ended, r.ID) },
	})
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	done := func(events map[conversationservice.SendEventKind][]conversationservice.SendEvent) conversation.Round {
		t.Helper()
		if len(events[conversationservice.EventDone]) != 1 {
			t.Fatalf("events %+v", events)
		}
		return *events[conversationservice.EventDone][0].Round
	}

	first := done(collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "hi"})))
	if first.Mode != "chat" || first.Reply != "hello" || len(chat.got[0].Tools) != 2 || !strings.Contains(chat.got[0].System, "# Tools") {
		t.Fatalf("the default mode's round %+v, call %+v", first, chat.got[0])
	}

	second := done(collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: first.ConversationID, Mode: "builder", Text: "build it"})))
	if second.Mode != "builder" || second.Reply != "built" {
		t.Fatalf("done = %+v", second)
	}
	if len(chat.got) != 4 || !strings.Contains(chat.got[1].System, "# Mode: Builder\nFollow the procedure.") || strings.Contains(chat.got[1].System, "# Tools") {
		t.Fatalf("calls = %d, system = %q", len(chat.got), chat.got[1].System)
	}
	if n := len(chat.got[1].Tools); n != 1 || chat.got[1].Tools[0].Name != "test_find" {
		t.Fatalf("tools = %+v", chat.got[1].Tools)
	}
	if chat.got[3].ToolChoice != chatservice.ToolChoiceNone {
		t.Fatalf("the third step was not made to answer: %+v", chat.got[3].ToolChoice)
	}
	// The history is the conversation's, whatever its mode
	if msgs := chat.got[1].Messages; len(msgs) != 3 || msgs[0].Content != "hi" || msgs[1].Content != "hello" {
		t.Fatalf("history = %+v", msgs)
	}
	if len(ended) != 2 || ended[1] != second.ID {
		t.Fatalf("ended = %v", ended)
	}

	// The conversation keeps the mode it was switched to
	third := done(collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: first.ConversationID, Text: "more"})))
	list := s.List(conversationservice.ListArgs{ServiceArgs: args})
	if third.Mode != "builder" || list.Conversations[0].Mode != "builder" {
		t.Fatalf("third round %+v, conversation %+v", third, list.Conversations[0])
	}
	if code := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Mode: "nope", Text: "x"}))[conversationservice.EventFailed][0].Common.Code; code != http.StatusBadRequest {
		t.Errorf("unknown mode: code %d", code)
	}
}

// A mode whose graph is missing or fails completes its round as failed.
func TestAFailingModeFailsItsRound(t *testing.T) {
	s, _ := newTestService(t, &fakeChat{})
	events := collect(s.Send(conversationservice.SendArgs{ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()}, Mode: "gone", Text: "x"}))
	done := events[conversationservice.EventDone]
	if len(done) != 1 || done[0].Round.Status != http.StatusInternalServerError || !strings.Contains(done[0].Round.Error, `no graph "nowhere"`) {
		t.Fatalf("events %+v", events)
	}
}

// Modes lists the chat.modes with their graphs' settings.
func TestModesListsTheModesGraphs(t *testing.T) {
	s, _ := newTestService(t, &fakeChat{})
	resp := s.Modes(conversationservice.ModesArgs{ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()}})
	if resp.Common.Code != http.StatusOK || len(resp.Modes) != 4 {
		t.Fatalf("modes %+v", resp)
	}
	chat, builder, gone := resp.Modes[0], resp.Modes[1], resp.Modes[3]
	if !chat.Default || strings.Join(chat.Settings, ",") != "test,tools" || chat.Missing {
		t.Errorf("chat %+v", chat)
	}
	if builder.Default || builder.Icon != "🛠" || !builder.Focus.Matches("prefab", "") || builder.Focus.Matches("markdown", "note") {
		t.Errorf("builder %+v", builder)
	}
	if !gone.Missing {
		t.Errorf("gone %+v", gone)
	}
}
