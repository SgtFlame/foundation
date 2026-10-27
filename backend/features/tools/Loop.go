package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools/settings"
	"gorm.io/gorm"
)

// ErrTimeout is what a Gate returns when a client tool's result did not
// arrive in time.
var ErrTimeout = errors.New("the browser did not answer in time")

// Gate is where a paused call waits: a chat round asks its user to approve
// a write and its browser to run a client tool. A run without one (an
// Agent node) denies writes and has no client tools.
type Gate interface {
	// Approve blocks until the user decides on call; an error (the round
	// was stopped) denies it.
	Approve(call ToolCall) (Approval, error)
	// RunClient has the browser run call and blocks for its result, at most
	// wait, or until the round is stopped for WaitForUser; ErrTimeout when
	// it does not come.
	RunClient(call ToolCall, wait time.Duration) (json.RawMessage, error)
}

// Loop is the tool loop (seam 4 in documentation/plans/Index.md): chat's
// Send runs it, and Cognition's Agent node wraps it. Each call is a
// ToolCall record, stored in DB when it ends.
type Loop struct {
	DB   *gorm.DB
	Chat chatservice.Service
}

// RunArgs is one round of the loop. CompleteArgs is the first model call:
// its ServiceArgs (whose Meta names the round or run), Level, System and
// Messages; Tools and ToolChoice are the loop's.
type RunArgs struct {
	chatservice.CompleteArgs
	Tools    []Tool // what the model may call, already filtered by policy
	Settings toolssettings.Tools
	// Allowed are the write tools allowed without asking (the
	// conversation's "Always allow"); an "always" approval adds to it.
	Allowed map[string]bool
	Gate    Gate
	// Stream streams each model call, sending its text to OnDelta; steps
	// after one that said something start with a blank line.
	Stream  bool
	OnDelta func(text string)
	// OnCall hears every change of a call's status, from running (or
	// awaiting, or client) to how it ended.
	OnCall func(call ToolCall)
	// Cite numbers a result's segments as the round's next citations; nil
	// leaves them unnumbered.
	Cite func(segments []retrieval.Scored) []retrieval.Scored
}

// Outcome is how the loop ended.
type Outcome struct {
	// Response is the last model call's; a failure's Common, else OK with
	// the last FinishReason and Model.
	Response chatservice.CompleteResponse
	Reply    string // every step's text, in order
	Calls    []ToolCall
	Usage    []chatservice.Usage // one per model call, to price each
	Steps    int                 // model calls made
}

// Run calls the model, runs the tools it asks for, and calls it again with
// their results, until it answers without calls. After Settings.MaxSteps
// steps with calls it must answer.
func (l Loop) Run(args RunArgs) Outcome {
	ctx := args.Context()
	specs, byWire := make([]chatservice.ToolSpec, 0, len(args.Tools)), map[string]Tool{}
	for _, t := range args.Tools {
		specs = append(specs, chatservice.ToolSpec{Name: wireName(t.ID), Description: t.Description, Parameters: t.Input})
		byWire[wireName(t.ID)] = t
	}
	if args.Allowed == nil {
		args.Allowed = map[string]bool{}
	}
	maxSteps := args.Settings.MaxSteps
	if maxSteps <= 0 {
		maxSteps = toolssettings.Defaults.MaxSteps
	}

	messages := slices.Clone(args.Messages)
	var out Outcome
	var texts []string
	for step := 1; ; step++ {
		req := args.CompleteArgs
		req.Messages = messages
		if len(specs) > 0 {
			req.Tools = specs
		}
		final := step > maxSteps
		if final {
			req.ToolChoice = chatservice.ToolChoiceNone
		}
		resp := l.complete(args, req, len(texts) > 0)
		out.Steps++
		out.Usage = append(out.Usage, resp.Usage)
		out.Response = resp
		if strings.TrimSpace(resp.Message.Content) != "" {
			texts = append(texts, resp.Message.Content)
		}
		if resp.Common.Code >= 300 {
			break
		}
		if len(resp.Message.ToolCalls) == 0 || final || ctx.Err() != nil {
			break
		}
		results := l.runCalls(args, step, resp.Message.ToolCalls, byWire, &out)
		messages = append(messages, resp.Message, chatservice.Message{Role: chatservice.RoleTool, Results: results})
		if ctx.Err() != nil {
			break
		}
	}
	out.Reply = strings.Join(texts, "\n\n")
	return out
}

// complete makes one model call, streamed when asked. A streamed call
// that fails (or is stopped) keeps the text it streamed as its Content.
func (l Loop) complete(args RunArgs, req chatservice.CompleteArgs, said bool) chatservice.CompleteResponse {
	if !args.Stream {
		return l.Chat.Complete(req)
	}
	stream := l.Chat.Stream(req)
	var text strings.Builder
	for d := range stream.Deltas {
		if d.Text == "" {
			continue
		}
		if text.Len() == 0 && said && args.OnDelta != nil {
			args.OnDelta("\n\n")
		}
		text.WriteString(d.Text)
		if args.OnDelta != nil {
			args.OnDelta(d.Text)
		}
	}
	resp := *<-stream.Response
	if resp.Common.Code >= 300 || resp.Message.Content == "" {
		resp.Message.Content = text.String()
	}
	return resp
}

// runCalls runs one step's calls and returns their results in order. Runs
// of calls that neither ask nor run in the browser go in parallel; the
// rest one at a time, so the user is asked one thing at once.
func (l Loop) runCalls(args RunArgs, step int, calls []chatservice.ToolCall, byWire map[string]Tool, out *Outcome) []chatservice.ToolResult {
	results := make([]chatservice.ToolResult, len(calls))
	records := make([]ToolCall, len(calls))
	quiet := func(c chatservice.ToolCall) bool {
		t, ok := byWire[c.Name]
		return !ok || (!t.Effect.Asks() && !t.Client())
	}
	for i := 0; i < len(calls); {
		j := i + 1
		if quiet(calls[i]) {
			for j < len(calls) && quiet(calls[j]) {
				j++
			}
		}
		var wg sync.WaitGroup
		for k := i; k < j; k++ {
			wg.Go(func() {
				records[k], results[k] = l.call(args, step, k+1, calls[k], byWire)
			})
		}
		wg.Wait()
		i = j
	}
	out.Calls = append(out.Calls, records...)
	return results
}

// call runs one call and records it.
func (l Loop) call(args RunArgs, step, n int, c chatservice.ToolCall, byWire map[string]Tool) (ToolCall, chatservice.ToolResult) {
	start := time.Now()
	meta := args.Meta
	tool, found := byWire[c.Name]
	rec := ToolCall{
		Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal),
		ConversationID: meta.ConversationID, RoundID: meta.RoundID, RunID: meta.RunID, NodeID: meta.NodeID,
		Step: step, CallID: "s" + strconv.Itoa(step) + "-" + strconv.Itoa(n), Name: c.Name, Args: string(argsOf(c.Args)),
	}
	if found {
		rec.Name, rec.Title, rec.Effect, rec.Location = tool.ID, tool.Title, tool.Effect, tool.Location
		if rec.Location == "" {
			rec.Location = LocationServer
		}
	}
	notify := func() {
		if args.OnCall != nil {
			args.OnCall(rec)
		}
	}
	end := func(status Status, content map[string]any, failure string) (ToolCall, chatservice.ToolResult) {
		if failure != "" {
			content = map[string]any{"error": failure}
		}
		body, size := l.cap(args.Settings, content)
		rec.Status, rec.Error, rec.Result, rec.ResultBytes = status, failure, string(body), size
		rec.DurationMs = time.Since(start).Milliseconds()
		if l.DB != nil {
			persist := context.WithoutCancel(args.Context())
			if err := l.DB.WithContext(persist).Create(&rec).Error; err != nil {
				Log.With("tool", rec.Name, "roundId", rec.RoundID).Error(persist, err, "cannot store the call of %s", rec.Name)
			}
		}
		notify()
		return rec, chatservice.ToolResult{CallID: c.ID, Name: c.Name, Result: body}
	}

	if !found {
		return end(StatusFailed, nil, fmt.Sprintf("There is no tool named %q.", c.Name))
	}
	if err := CheckArgs(tool.Input, c.Args); err != nil {
		return end(StatusFailed, nil, "The call does not match the tool's input schema: "+err.Error()+". Correct the arguments and call it again.")
	}

	rec.Approval = ApprovalAuto
	if tool.Effect.Asks() {
		switch {
		case args.Allowed[tool.ID]:
			rec.Approval = ApprovalAlways
		case args.Gate == nil:
			rec.Approval = ApprovalDenied
			return end(StatusDenied, nil, "This tool changes the user's data and needs their approval, which this run cannot ask for.")
		default:
			rec.Status = StatusAwaiting
			notify()
			approval, err := args.Gate.Approve(rec)
			if err != nil || approval == ApprovalDenied || approval == "" {
				rec.Approval = ApprovalDenied
				return end(StatusDenied, nil, "The user denied this call. Do not try it again; ask what they would like instead.")
			}
			rec.Approval = approval
			if approval == ApprovalAlways {
				args.Allowed[tool.ID] = true
			}
		}
	}

	if tool.Client() {
		if args.Gate == nil {
			return end(StatusFailed, nil, "This tool runs in the user's browser, which this run does not have.")
		}
		wait := tool.Wait
		if wait == 0 {
			wait = args.Settings.ClientTimeout()
		}
		rec.Status = StatusClient
		notify()
		raw, err := args.Gate.RunClient(rec, wait)
		switch {
		case errors.Is(err, ErrTimeout):
			return end(StatusTimeout, nil, "The user's browser did not answer in time.")
		case err != nil:
			return end(StatusFailed, nil, err.Error())
		}
		return end(StatusDone, objectOf(raw), "")
	}

	if tool.Run == nil {
		return end(StatusFailed, nil, "This tool cannot be run.")
	}
	rec.Status = StatusRunning
	notify()
	ctx, cancel := context.WithTimeout(args.Context(), args.Settings.Timeout())
	defer cancel()
	sa := args.ServiceArgs
	sa.Ctx = ctx
	sa.Meta.Feature = "tool." + tool.ID
	res, err := run(tool, Call{ServiceArgs: sa, ID: rec.CallID, Name: tool.ID, Args: argsOf(c.Args)})
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return end(StatusTimeout, nil, fmt.Sprintf("The tool did not finish within %s.", args.Settings.Timeout()))
	case err != nil:
		return end(StatusFailed, nil, err.Error())
	}
	rec.Summary = res.Summary
	content := res.Content
	if len(res.Segments) > 0 {
		segments := res.Segments
		if args.Cite != nil {
			segments = args.Cite(segments)
			numbers := make([]int, 0, len(segments))
			for _, s := range segments {
				numbers = append(numbers, s.Number)
			}
			cited, _ := json.Marshal(numbers)
			rec.Cited = string(cited)
		}
		content = withPassages(content, segments)
	}
	return end(StatusDone, content, "")
}

// run runs a server tool, turning a panic into its failure.
func run(tool Tool, call Call) (res Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			Log.With("tool", tool.ID).Error(call.Context(), fmt.Errorf("%v", p), "tool %s panicked", tool.ID)
			res, err = Result{}, errors.New("the tool failed unexpectedly")
		}
	}()
	return tool.Run(call)
}

// cap is content as JSON, cut to the settings' maxResultKB with a note to
// the model; size is its length before.
func (l Loop) cap(settings toolssettings.Tools, content map[string]any) (json.RawMessage, int) {
	if content == nil {
		content = map[string]any{}
	}
	body, err := json.Marshal(content)
	if err != nil {
		body, _ = json.Marshal(map[string]any{"error": "the tool's result could not be encoded"})
	}
	limit := settings.MaxResultKB
	if limit <= 0 {
		limit = toolssettings.Defaults.MaxResultKB
	}
	size := len(body)
	if size <= limit<<10 {
		return body, size
	}
	cut, _ := json.Marshal(map[string]any{
		"truncated": true,
		"note":      fmt.Sprintf("The result was %d KB, more than the %d KB you may see, so only its start is here. Ask for less: fewer results or a line range.", size>>10, limit),
		"partial":   string(body[:limit<<10]),
	})
	return cut, size
}

// withPassages adds the segments to content as "passages", each with its
// citation when it has one.
func withPassages(content map[string]any, segments []retrieval.Scored) map[string]any {
	if content == nil {
		content = map[string]any{}
	}
	passages := make([]map[string]any, 0, len(segments))
	for _, s := range segments {
		p := map[string]any{"title": s.Title, "text": s.Text}
		if s.Number > 0 {
			p["cite"] = "[" + strconv.Itoa(s.Number) + "]"
		}
		if s.Location.Path != "" {
			p["path"] = s.Location.Path
		}
		if s.Location.StartLine > 0 {
			p["lines"] = fmt.Sprintf("%d-%d", s.Location.StartLine, s.Location.EndLine)
		}
		if s.Location.EntryID != 0 {
			p["entryId"] = strconv.FormatUint(s.Location.EntryID, 10)
		}
		passages = append(passages, p)
	}
	content["passages"] = passages
	return content
}

// argsOf is a call's arguments, {} when it has none.
func argsOf(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}

// objectOf is a client tool's JSON result as an object.
func objectOf(raw json.RawMessage) map[string]any {
	var out map[string]any
	if json.Unmarshal(raw, &out) == nil && out != nil {
		return out
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return map[string]any{"result": v}
}

// Enabled is the tools offered: every registered and client tool not
// turned off in the settings, client tools only when the run has a
// browser. A client tool never shadows a server tool's name.
func Enabled(server []Tool, client []Tool, settings toolssettings.Tools) []Tool {
	var out []Tool
	seen := map[string]bool{}
	for _, t := range slices.Concat(server, client) {
		if seen[t.ID] || slices.Contains(settings.Disabled, t.ID) {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out
}
