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

func nodeOf(t *testing.T, reg *extensions.Registry, id string) cognition.NodeType {
	t.Helper()
	for _, n := range cognition.Nodes.All(reg) {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %s", id)
	return cognition.NodeType{}
}

// The Agent node gives each call's arguments and result, and the run's
// usage, at the level its pin names (forge 7m: an agent run's record).
func TestAgentGivesArgumentsResultsAndUsage(t *testing.T) {
	reg := extensions.NewRegistry()
	chat := &twoSteps{}
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.read"}, Effect: tools.EffectRead,
		Run: func(tools.Call) (tools.Result, error) { return tools.Result{Content: map[string]any{"found": 2}}, nil }})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.write"}, Effect: tools.EffectWrite})
	Contribute(reg, func() Deps { return Deps{Chat: chat, Registry: reg} })
	sa := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	node := cognition.Node{ID: "a", Type: AgentID, Config: json.RawMessage(`{"tools": ["t.read", "t.write"]}`)}
	out, _, err := nodeOf(t, reg, AgentID).Run(cognition.NewExec(sa, 1, node, cognition.Values{}), cognition.Values{"prompt": "go", "level": "fast"})
	if err != nil {
		t.Fatal(err)
	}
	calls := out["calls"].([]map[string]any)
	if res, _ := calls[0]["result"].(map[string]any); res["found"] != float64(2) || calls[0]["args"] == nil {
		t.Errorf("the read call: %v", calls[0])
	}
	if u := out["usage"].(map[string]any); u["steps"] != 2 || u["level"] != "fast" {
		t.Errorf("usage: %v", u)
	}
	if chat.got[0].Level != "fast" {
		t.Errorf("asked at %q", chat.got[0].Level)
	}
}

// Tools' Include pin selects by name in place of the config, and an empty
// list joined selects nothing (forge 7m: an agent with no tools).
func TestToolsIncludePin(t *testing.T) {
	reg := extensions.NewRegistry()
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.read"}, Effect: tools.EffectRead})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.other"}, Effect: tools.EffectRead})
	Contribute(reg, func() Deps { return Deps{Registry: reg} })
	sel := nodeOf(t, reg, SelectID)
	sa := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	run := func(in cognition.Values) []tools.Spec {
		out, _, err := sel.Run(cognition.NewExec(sa, 1, cognition.Node{ID: "s", Type: SelectID}, cognition.Values{}), in)
		if err != nil {
			t.Fatal(err)
		}
		return out["tools"].([]tools.Spec)
	}
	if got := run(cognition.Values{"include": []any{"t.read"}}); len(got) != 1 || got[0].Name != "t.read" {
		t.Errorf("include t.read: %+v", got)
	}
	if got := run(cognition.Values{"include": []any{}}); len(got) != 0 {
		t.Errorf("an empty include: %+v", got)
	}
	if got := run(cognition.Values{}); len(got) != 2 {
		t.Errorf("no include: %+v", got)
	}
}

// Call tool runs a server tool by name as the run's principal; a write
// is refused unless the run is an agent's; an unknown tool or bad
// arguments leave by Failed.
func TestCallTool(t *testing.T) {
	reg := extensions.NewRegistry()
	var by []features.Principal
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.read"}, Effect: tools.EffectRead,
		Input: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
		Run: func(c tools.Call) (tools.Result, error) {
			by = append(by, c.Principal)
			return tools.Result{Content: map[string]any{"text": "hello"}, Summary: "read"}, nil
		}})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "t.write"}, Effect: tools.EffectWrite,
		Run: func(c tools.Call) (tools.Result, error) {
			return tools.Result{Content: map[string]any{"wrote": true}}, nil
		}})
	Contribute(reg, func() Deps { return Deps{Registry: reg} })
	call := nodeOf(t, reg, CallToolID)
	user := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	agent := features.ServiceArgs{Ctx: context.Background(), Principal: features.AgentPrincipal(1, 7, 1)}
	run := func(sa features.ServiceArgs, in cognition.Values) (cognition.Values, string) {
		out, exec, err := call.Run(cognition.NewExec(sa, 1, cognition.Node{ID: "c", Type: CallToolID}, cognition.Values{}), in)
		if err != nil {
			t.Fatal(err)
		}
		return out, exec
	}
	if out, exec := run(user, cognition.Values{"name": "t.read", "args": map[string]any{"id": "1"}}); exec != cognition.Then || out["result"].(map[string]any)["text"] != "hello" || out["summary"] != "read" {
		t.Errorf("a read: %v %s", out, exec)
	}
	if out, exec := run(user, cognition.Values{"name": "t.read", "args": map[string]any{}}); exec != "failed" || out["error"] == "" {
		t.Errorf("bad arguments: %v %s", out, exec)
	}
	if out, exec := run(user, cognition.Values{"name": "t.nope"}); exec != "failed" || out["error"] == "" {
		t.Errorf("an unknown tool: %v %s", out, exec)
	}
	if _, exec := run(user, cognition.Values{"name": "t.write"}); exec != "failed" {
		t.Error("a graph wrote without asking, as a user")
	}
	if out, exec := run(agent, cognition.Values{"name": "t.write"}); exec != cognition.Then || out["result"].(map[string]any)["wrote"] != true {
		t.Errorf("an agent's write: %v %s", out, exec)
	}
	if len(by) != 1 || by[0].Kind != features.PrincipalUser {
		t.Errorf("the read ran as %v", by)
	}
}
