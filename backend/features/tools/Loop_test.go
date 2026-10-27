package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools/settings"
)

// scriptChat answers each model call with the next reply of its script,
// and records what it was sent.
type scriptChat struct {
	mu     sync.Mutex
	script []chatservice.Message
	got    []chatservice.CompleteArgs
}

func (s *scriptChat) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, args)
	msg := chatservice.Message{Role: chatservice.RoleAssistant, Content: "out of script"}
	if len(s.script) > 0 {
		msg, s.script = s.script[0], s.script[1:]
	}
	return chatservice.CompleteResponse{Common: features.OK(), Message: msg, Model: "m", FinishReason: "stop", Usage: chatservice.Usage{InputTokens: 10, OutputTokens: 2}}
}

func (s *scriptChat) Stream(args chatservice.StreamArgs) *chatservice.StreamRequest {
	resp := s.Complete(args)
	deltas := make(chan chatservice.Delta, 1)
	if resp.Message.Content != "" {
		deltas <- chatservice.Delta{Text: resp.Message.Content}
	}
	close(deltas)
	out := make(chan *chatservice.CompleteResponse, 1)
	out <- &resp
	return &chatservice.StreamRequest{Deltas: deltas, Response: out}
}

func calls(cs ...chatservice.ToolCall) chatservice.Message {
	return chatservice.Message{Role: chatservice.RoleAssistant, ToolCalls: cs}
}

func answer(text string) chatservice.Message {
	return chatservice.Message{Role: chatservice.RoleAssistant, Content: text}
}

func callOf(name, args string) chatservice.ToolCall {
	return chatservice.ToolCall{ID: name + "-id", Name: name, Args: json.RawMessage(args)}
}

type gate struct {
	approval Approval
	client   json.RawMessage
	asked    []ToolCall
}

func (g *gate) Approve(call ToolCall) (Approval, error) {
	g.asked = append(g.asked, call)
	return g.approval, nil
}

func (g *gate) RunClient(call ToolCall, wait time.Duration) (json.RawMessage, error) {
	if g.client == nil {
		return nil, ErrTimeout
	}
	return g.client, nil
}

var (
	echo = Tool{
		Descriptor: extensions.Descriptor{ID: "test.echo"}, Effect: EffectRead,
		Input: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		Run: func(call Call) (Result, error) {
			args, err := Decode[struct{ Text string }](call)
			return Result{Content: map[string]any{"echo": args.Text}, Summary: "echoed"}, err
		},
	}
	write = Tool{
		Descriptor: extensions.Descriptor{ID: "test.write"}, Effect: EffectWrite,
		Run: func(call Call) (Result, error) { return Result{Content: map[string]any{"ok": true}}, nil },
	}
	search = Tool{
		Descriptor: extensions.Descriptor{ID: "test.search"}, Effect: EffectRead,
		Run: func(call Call) (Result, error) {
			return Result{Segments: []retrieval.Scored{{Segment: retrieval.Segment{Title: "notes", Text: "hello", Location: retrieval.Location{Path: "v/notes.md", StartLine: 1, EndLine: 3}}}}}, nil
		},
	}
	pick = Tool{Descriptor: extensions.Descriptor{ID: "chat.askChoice"}, Effect: EffectUI, Location: LocationClient, Wait: WaitForUser}
)

func newLoop(t *testing.T, chat *scriptChat) (Loop, RunArgs) {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, Models()...); err != nil {
		t.Fatal(err)
	}
	sa := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal(), Meta: features.Meta{Feature: "chat", RoundID: 7}}
	return Loop{DB: gdb, Chat: chat}, RunArgs{
		CompleteArgs: chatservice.CompleteArgs{ServiceArgs: sa, Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: "hi"}}},
		Settings:     toolssettings.Defaults,
	}
}

func TestLoopRunsToolsAndAnswers(t *testing.T) {
	chat := &scriptChat{script: []chatservice.Message{
		{Role: chatservice.RoleAssistant, Content: "Looking.", ToolCalls: []chatservice.ToolCall{callOf("test_echo", `{"text":"a"}`), callOf("test_echo", `{"text":"b"}`)}},
		answer("Done."),
	}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{echo}
	args.Stream = true
	var streamed strings.Builder
	args.OnDelta = func(s string) { streamed.WriteString(s) }
	out := loop.Run(args)

	if out.Response.Common.Code != 200 || out.Reply != "Looking.\n\nDone." || streamed.String() != out.Reply || out.Steps != 2 {
		t.Fatalf("outcome = %+v, streamed %q", out, streamed.String())
	}
	if len(out.Calls) != 2 || out.Calls[0].Status != StatusDone || out.Calls[0].Summary != "echoed" || out.Calls[1].CallID != "s1-2" {
		t.Fatalf("calls = %+v", out.Calls)
	}
	second := chat.got[1].Messages
	if len(second) != 3 || second[2].Role != chatservice.RoleTool || string(second[2].Results[1].Result) != `{"echo":"b"}` || second[2].Results[0].CallID != "test_echo-id" {
		t.Fatalf("second call's messages = %+v", second)
	}
	if chat.got[0].Tools[0].Name != "test_echo" {
		t.Fatalf("tools sent = %+v", chat.got[0].Tools)
	}
	var stored int64
	loop.DB.Model(&ToolCall{}).Where("round_id = ?", 7).Count(&stored)
	if stored != 2 {
		t.Fatalf("stored %d calls", stored)
	}
}

func TestLoopChecksArgumentsAndNames(t *testing.T) {
	chat := &scriptChat{script: []chatservice.Message{calls(callOf("test_echo", `{"text":3}`), callOf("nope", `{}`)), answer("ok")}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{echo}
	out := loop.Run(args)
	if out.Calls[0].Status != StatusFailed || !strings.Contains(out.Calls[0].Error, "text must be a string") {
		t.Fatalf("bad arguments: %+v", out.Calls[0])
	}
	if out.Calls[1].Status != StatusFailed || !strings.Contains(out.Calls[1].Error, "no tool") {
		t.Fatalf("unknown tool: %+v", out.Calls[1])
	}
}

func TestLoopAsksBeforeWriting(t *testing.T) {
	for _, c := range []struct {
		name     string
		gate     *gate
		allowed  bool
		status   Status
		approval Approval
	}{
		{"approved", &gate{approval: ApprovalApproved}, false, StatusDone, ApprovalApproved},
		{"denied", &gate{approval: ApprovalDenied}, false, StatusDenied, ApprovalDenied},
		{"no gate", nil, false, StatusDenied, ApprovalDenied},
		{"always allowed", &gate{}, true, StatusDone, ApprovalAlways},
	} {
		t.Run(c.name, func(t *testing.T) {
			chat := &scriptChat{script: []chatservice.Message{calls(callOf("test_write", `{}`)), answer("ok")}}
			loop, args := newLoop(t, chat)
			args.Tools = []Tool{write}
			if c.gate != nil {
				args.Gate = c.gate
			}
			if c.allowed {
				args.Allowed = map[string]bool{"test.write": true}
			}
			var seen []Status
			args.OnCall = func(call ToolCall) { seen = append(seen, call.Status) }
			out := loop.Run(args)
			if got := out.Calls[0]; got.Status != c.status || got.Approval != c.approval {
				t.Fatalf("call = %+v", got)
			}
			if c.gate != nil && !c.allowed && (len(seen) == 0 || seen[0] != StatusAwaiting) {
				t.Fatalf("statuses = %v", seen)
			}
		})
	}
}

func TestLoopAlwaysAllowsLaterCalls(t *testing.T) {
	chat := &scriptChat{script: []chatservice.Message{calls(callOf("test_write", `{}`)), calls(callOf("test_write", `{}`)), answer("ok")}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{write}
	g := &gate{approval: ApprovalAlways}
	args.Gate = g
	args.Allowed = map[string]bool{}
	out := loop.Run(args)
	if len(g.asked) != 1 || out.Calls[1].Approval != ApprovalAlways || !args.Allowed["test.write"] {
		t.Fatalf("asked %d times, calls %+v", len(g.asked), out.Calls)
	}
}

func TestLoopClientTools(t *testing.T) {
	chat := &scriptChat{script: []chatservice.Message{calls(callOf("chat_askChoice", `{}`)), answer("ok")}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{pick}
	args.Gate = &gate{client: json.RawMessage(`{"choice":"Mike"}`)}
	out := loop.Run(args)
	if out.Calls[0].Status != StatusDone || out.Calls[0].Result != `{"choice":"Mike"}` {
		t.Fatalf("call = %+v", out.Calls[0])
	}

	chat = &scriptChat{script: []chatservice.Message{calls(callOf("chat_askChoice", `{}`)), answer("ok")}}
	loop, args = newLoop(t, chat)
	args.Tools = []Tool{pick}
	args.Gate = &gate{}
	if out := loop.Run(args); out.Calls[0].Status != StatusTimeout {
		t.Fatalf("timed out call = %+v", out.Calls[0])
	}
}

func TestLoopCitesSegments(t *testing.T) {
	chat := &scriptChat{script: []chatservice.Message{calls(callOf("test_search", `{}`)), answer("see [4]")}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{search}
	args.Cite = func(segments []retrieval.Scored) []retrieval.Scored {
		for i := range segments {
			segments[i].Number = 4 + i
		}
		return segments
	}
	out := loop.Run(args)
	if out.Calls[0].Cited != "[4]" || !strings.Contains(out.Calls[0].Result, `"cite":"[4]"`) || !strings.Contains(out.Calls[0].Result, `"lines":"1-3"`) {
		t.Fatalf("call = %+v", out.Calls[0])
	}
}

func TestLoopStopsAfterMaxSteps(t *testing.T) {
	chat := &scriptChat{script: []chatservice.Message{
		calls(callOf("test_echo", `{"text":"1"}`)), calls(callOf("test_echo", `{"text":"2"}`)), {Role: chatservice.RoleAssistant, Content: "final", ToolCalls: []chatservice.ToolCall{callOf("test_echo", `{"text":"3"}`)}},
	}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{echo}
	args.Settings.MaxSteps = 2
	out := loop.Run(args)
	if out.Steps != 3 || len(out.Calls) != 2 || chat.got[2].ToolChoice != chatservice.ToolChoiceNone || out.Reply != "final" {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestLoopCutsLargeResults(t *testing.T) {
	big := Tool{Descriptor: extensions.Descriptor{ID: "test.big"}, Effect: EffectRead, Run: func(Call) (Result, error) {
		return Result{Content: map[string]any{"text": strings.Repeat("x", 3000)}}, nil
	}}
	chat := &scriptChat{script: []chatservice.Message{calls(callOf("test_big", `{}`)), answer("ok")}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{big}
	args.Settings.MaxResultKB = 1
	out := loop.Run(args)
	if c := out.Calls[0]; c.ResultBytes < 3000 || !strings.Contains(c.Result, `"truncated":true`) || len(c.Result) > 1500 {
		t.Fatalf("call = %d bytes, %d stored", c.ResultBytes, len(c.Result))
	}
}

func TestCheckArgs(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
		"n":{"type":"integer","minimum":1,"maximum":5},
		"mode":{"type":"string","enum":["a","b"]},
		"edits":{"type":"array","minItems":1,"items":{"type":"object","properties":{"find":{"type":"string","minLength":1}},"required":["find"]}}
	},"required":["n"]}`)
	for args, want := range map[string]string{
		`{"n":2}`:                       "",
		`{}`:                            "n is required",
		`{"n":2.5}`:                     "whole number",
		`{"n":9}`:                       "at most 5",
		`{"n":1,"mode":"c"}`:            "one of",
		`{"n":1,"edits":[]}`:            "at least 1 items",
		`{"n":1,"edits":[{}]}`:          "edits[0].find is required",
		`{"n":1,"edits":[{"find":""}]}`: "at least 1 characters",
		`[1]`:                           "must be a JSON object",
	} {
		err := CheckArgs(schema, json.RawMessage(args))
		if (want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("CheckArgs(%s) = %v, want %q", args, err, want)
		}
	}
}

func TestLoopSurvivesBrokenTools(t *testing.T) {
	broken := Tool{Descriptor: extensions.Descriptor{ID: "test.panic"}, Effect: EffectRead, Run: func(Call) (Result, error) { panic("boom") }}
	empty := Tool{Descriptor: extensions.Descriptor{ID: "test.empty"}, Effect: EffectRead}
	chat := &scriptChat{script: []chatservice.Message{calls(callOf("test_panic", `{}`), callOf("test_empty", `{}`)), answer("ok")}}
	loop, args := newLoop(t, chat)
	args.Tools = []Tool{broken, empty}
	out := loop.Run(args)
	if out.Reply != "ok" || out.Calls[0].Status != StatusFailed || out.Calls[1].Status != StatusFailed {
		t.Fatalf("outcome = %+v", out)
	}
}
