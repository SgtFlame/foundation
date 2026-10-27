package toolsnodes

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// twoSteps asks for a read and a write, then answers.
type twoSteps struct{ got []chatservice.CompleteArgs }

func (c *twoSteps) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	c.got = append(c.got, args)
	msg := chatservice.Message{Role: chatservice.RoleAssistant, Content: "answer"}
	if len(c.got) == 1 {
		msg = chatservice.Message{Role: chatservice.RoleAssistant, ToolCalls: []chatservice.ToolCall{{Name: "t_read", Args: json.RawMessage(`{}`)}, {Name: "t_write", Args: json.RawMessage(`{}`)}}}
	}
	return chatservice.CompleteResponse{Common: features.OK(), Message: msg}
}

func (c *twoSteps) Stream(args chatservice.StreamArgs) *chatservice.StreamRequest {
	panic("not streamed")
}

func TestAgentRunsToolsAndDeniesWrites(t *testing.T) {
	reg := extensions.NewRegistry()
	chat := &twoSteps{}
	wrote := false
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.read"}, Effect: tools.EffectRead,
		Run: func(tools.Call) (tools.Result, error) { return tools.Result{Summary: "read"}, nil }})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.write"}, Effect: tools.EffectWrite,
		Run: func(tools.Call) (tools.Result, error) { wrote = true; return tools.Result{}, nil }})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.other"}, Effect: tools.EffectRead})
	Contribute(reg, func() Deps { return Deps{Chat: chat, Registry: reg} })

	agent := cognition.Nodes.All(reg)[0]
	node := cognition.Node{ID: "a", Type: AgentID, Config: json.RawMessage(`{"tools": ["t.read", "t.write"]}`)}
	sa := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	out, exec, err := agent.Run(cognition.NewExec(sa, 42, node, cognition.Values{}), cognition.Values{"prompt": "go"})
	if err != nil || exec != cognition.Then || out["reply"] != "answer" {
		t.Fatalf("Run = %v, %q, %v", out, exec, err)
	}
	calls := out["calls"].([]map[string]any)
	if len(calls) != 2 || calls[0]["status"] != tools.StatusDone || calls[1]["status"] != tools.StatusDenied || wrote {
		t.Fatalf("calls = %v, wrote %v", calls, wrote)
	}
	if len(chat.got[0].Tools) != 2 || chat.got[0].Meta.RunID != 42 || chat.got[0].Meta.NodeID != "a" || chat.got[0].Meta.Feature != "agent" {
		t.Fatalf("first call = %+v", chat.got[0])
	}
}

func TestAgentLeavesOutWritesByDefault(t *testing.T) {
	reg := extensions.NewRegistry()
	chat := &twoSteps{}
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.read"}, Effect: tools.EffectRead})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.write"}, Effect: tools.EffectWrite})
	Contribute(reg, func() Deps { return Deps{Chat: chat, Registry: reg} })
	agent := cognition.Nodes.All(reg)[0]
	sa := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	if _, _, err := agent.Run(cognition.NewExec(sa, 1, cognition.Node{ID: "a", Type: AgentID}, cognition.Values{}), cognition.Values{"prompt": "go"}); err != nil {
		t.Fatal(err)
	}
	if got := chat.got[0].Tools; len(got) != 1 || got[0].Name != "t_read" {
		t.Fatalf("offered %+v", got)
	}
}
