package toolsnodes

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// answers replies with text, recording what it was asked.
type answers struct {
	text string
	got  []chatservice.CompleteArgs
}

func (c *answers) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	c.got = append(c.got, args)
	return chatservice.CompleteResponse{Common: features.OK(), Message: chatservice.Message{Role: chatservice.RoleAssistant, Content: c.text}, Level: args.Level}
}

func (c *answers) Stream(chatservice.StreamArgs) *chatservice.StreamRequest { panic("not streamed") }

// sections answers config reads from fixed values.
type sections map[string]string

func (s sections) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	if v, ok := s[section]; ok {
		return json.RawMessage(v), nil
	}
	return nil, errNoSection
}

var errNoSection = &noSection{}

type noSection struct{}

func (*noSection) Error() string { return "no such section" }

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

// Ask model sends its model settings, the pin winning over the config,
// and leaves the rest to the level (forge 15b).
func TestAskModelSendsItsSettings(t *testing.T) {
	reg := extensions.NewRegistry()
	chat := &answers{text: "hi"}
	Contribute(reg, func() Deps { return Deps{Chat: chat, Registry: reg} })
	ask := nodeOf(t, reg, CompleteID)
	node := cognition.Node{ID: "ask", Type: CompleteID, Config: json.RawMessage(`{"level":"fast","model":"flash","temperature":0.7,"maxOutputTokens":8,"thinkingBudget":0}`)}
	out, exec, err := ask.Run(cognition.NewExec(args(), 1, node, cognition.Values{}), cognition.Values{"prompt": "go", "temperature": 0.0, "model": "pro"})
	if err != nil || exec != cognition.Then || out["text"] != "hi" {
		t.Fatalf("Run = %v %q %v", out, exec, err)
	}
	got := chat.got[0]
	if got.Level != "fast" || got.ModelRef != "pro" || got.Temperature == nil || *got.Temperature != 0 || got.MaxTokens == nil || *got.MaxTokens != 8 ||
		got.ThinkingBudget == nil || *got.ThinkingBudget != 0 {
		t.Fatalf("sent %+v", got)
	}
	// Nothing set leaves everything to the level
	chat.got = nil
	if _, _, err := ask.Run(cognition.NewExec(args(), 1, cognition.Node{ID: "ask", Type: CompleteID}, cognition.Values{}), cognition.Values{"prompt": "go"}); err != nil {
		t.Fatal(err)
	}
	if g := chat.got[0]; g.ModelRef != "" || g.Temperature != nil || g.MaxTokens != nil || g.ThinkingBudget != nil {
		t.Fatalf("unset settings were sent: %+v", g)
	}
}

// With a JSON schema, Ask model asks for JSON and gives it parsed.
func TestAskModelGivesJSONValue(t *testing.T) {
	reg := extensions.NewRegistry()
	chat := &answers{text: "```json\n{\"yes\": true}\n```"}
	Contribute(reg, func() Deps { return Deps{Chat: chat, Registry: reg} })
	ask := nodeOf(t, reg, CompleteID)
	node := cognition.Node{ID: "ask", Type: CompleteID, Config: json.RawMessage(`{"jsonSchema":"{\"type\":\"object\",\"properties\":{\"yes\":{\"type\":\"boolean\"}}}"}`)}
	out, exec, err := ask.Run(cognition.NewExec(args(), 1, node, cognition.Values{}), cognition.Values{"prompt": "go"})
	if err != nil || exec != cognition.Then {
		t.Fatalf("Run = %v %q %v", out, exec, err)
	}
	if v, _ := out["value"].(map[string]any); v["yes"] != true || len(chat.got[0].JSONSchema) == 0 {
		t.Fatalf("value = %v, schema sent %s", out["value"], chat.got[0].JSONSchema)
	}
	chat.text = "not json"
	if out, exec, _ := ask.Run(cognition.NewExec(args(), 1, node, cognition.Values{}), cognition.Values{"prompt": "go"}); exec != "failed" || out["error"] == "" {
		t.Fatalf("a reply that is not JSON: %v %q", out, exec)
	}
}

// The Agent node's Retrieval group sets kb.search's options: the preset,
// then its dials, then the tenant's default; the pin narrows (forge 15b).
func TestAgentRetrievalGroup(t *testing.T) {
	reg := extensions.NewRegistry()
	var seen []retrieval.Options
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "kb.search"}, Effect: tools.EffectRead,
		Run: func(c tools.Call) (tools.Result, error) {
			o, _ := retrieval.OptionsFrom(c.Context())
			seen = append(seen, o)
			return tools.Result{}, nil
		}})
	chat := &searchOnce{}
	cfg := sections{
		"chat.context.presets":      `{"presets":[{"id":"brainstorm","title":"Explore ideas","lexicalSemantic":4,"chronicleLibrary":0,"topK":12}]}`,
		"chat.context.defaults.rag": `{"enabled":true,"lexicalSemantic":1,"chronicleLibrary":2,"topK":8,"sources":{"library":true,"chronicle":true,"records":false}}`,
	}
	Contribute(reg, func() Deps { return Deps{Chat: chat, Registry: reg, Config: cfg} })
	agent := nodeOf(t, reg, AgentID)
	run := func(config string, in cognition.Values) retrieval.Options {
		t.Helper()
		seen, chat.n = nil, 0
		in["prompt"] = "go"
		if _, _, err := agent.Run(cognition.NewExec(args(), 1, cognition.Node{ID: "a", Type: AgentID, Config: json.RawMessage(config)}, cognition.Values{}), in); err != nil {
			t.Fatal(err)
		}
		if len(seen) != 1 {
			t.Fatalf("kb.search ran %d times", len(seen))
		}
		return seen[0]
	}
	o := run(`{"tools":["kb.search"],"rag":{"preset":"brainstorm","topK":4}}`, cognition.Values{})
	if o.LexicalSemantic != 4 || o.ChronicleLibrary != 0 || o.TopK != 4 {
		t.Fatalf("preset and dial: %+v", o)
	}
	o = run(`{"tools":["kb.search"]}`, cognition.Values{"retrieval": map[string]any{"volumeIds": []any{"7"}}})
	if o.LexicalSemantic != 1 || o.ChronicleLibrary != 2 || o.TopK != 8 || len(o.VolumeIDs) != 1 || o.VolumeIDs[0] != 7 {
		t.Fatalf("an empty group with the run's Volumes: %+v", o)
	}
	o = run(`{"tools":["kb.search"],"rag":{"sources":{"chronicle":false}}}`, cognition.Values{"retrieval": map[string]any{"sources": []any{"library", "chronicle", "records"}}})
	if len(o.Sources) != 2 || o.Sources[0] != "none" || o.Sources[1] != "library" {
		t.Fatalf("the pin widened the sources: %+v", o.Sources)
	}
}

// searchOnce calls kb.search once, then answers.
type searchOnce struct{ n int }

func (c *searchOnce) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	c.n++
	msg := chatservice.Message{Role: chatservice.RoleAssistant, Content: "done"}
	if c.n == 1 {
		msg = chatservice.Message{Role: chatservice.RoleAssistant, ToolCalls: []chatservice.ToolCall{{Name: "kb_search", Args: json.RawMessage(`{}`)}}}
	}
	return chatservice.CompleteResponse{Common: features.OK(), Message: msg}
}

func (c *searchOnce) Stream(chatservice.StreamArgs) *chatservice.StreamRequest { panic("not streamed") }

func TestResolve(t *testing.T) {
	four := 4
	presets := []retrieval.Preset{{ID: "code", LexicalSemantic: -4, ChronicleLibrary: 3, TopK: 8}}
	def := retrieval.Options{LexicalSemantic: 1, ChronicleLibrary: 1, TopK: 6, Sources: []string{"none", "library", "chronicle"}}
	if o := retrieval.Resolve(retrieval.Dials{}, presets, def); o.LexicalSemantic != 1 || o.TopK != 6 || len(o.Sources) != 3 {
		t.Fatalf("empty: %+v", o)
	}
	if o := retrieval.Resolve(retrieval.Dials{Preset: "code", TopK: &four}, presets, def); o.LexicalSemantic != -4 || o.ChronicleLibrary != 3 || o.TopK != 4 {
		t.Fatalf("preset then dial: %+v", o)
	}
	narrowed := def.Narrow(retrieval.Options{Sources: []string{"records", "library"}, VolumeIDs: []uint64{3}})
	if len(narrowed.Sources) != 2 || narrowed.Sources[1] != "library" || narrowed.VolumeIDs[0] != 3 {
		t.Fatalf("narrow: %+v", narrowed)
	}
}
