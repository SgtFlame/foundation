package conversationdto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ToolCall is one tool call of a round, as its card shows it. The data of
// a "tool" event, sent each time the call's status changes: awaiting asks
// the user to approve it, client asks the browser to run it, and both
// wait for POST /conversation/answer.
type ToolCall struct {
	CallID     string          `json:"callId"` // unique in its round
	Step       int             `json:"step"`
	Name       string          `json:"name"`
	Title      string          `json:"title,omitempty"`
	Effect     string          `json:"effect,omitempty"`   // none, read, write, external or ui
	Location   string          `json:"location,omitempty"` // server or client
	Args       json.RawMessage `json:"args"`
	Status     string          `json:"status"`             // running, awaiting, client, done, failed, denied or timeout
	Approval   string          `json:"approval,omitempty"` // auto, approved, always or denied
	Summary    string          `json:"summary,omitempty"`  // one line of markdown
	Error      string          `json:"error,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"` // what the model saw, once it ended
	Cited      []int           `json:"cited,omitempty"`  // the citations the result added
	DurationMs int64           `json:"durationMs,omitempty"`
	Time       *time.Time      `json:"time,omitempty"` // when it ended
}

func ToolCallFrom(c tools.ToolCall) ToolCall {
	out := ToolCall{
		CallID: c.CallID, Step: c.Step, Name: c.Name, Title: c.Title, Effect: string(c.Effect), Location: string(c.Location),
		Args: json.RawMessage(c.Args), Status: string(c.Status), Approval: string(c.Approval), Summary: c.Summary, Error: c.Error, DurationMs: c.DurationMs,
	}
	if !json.Valid(out.Args) {
		out.Args = json.RawMessage("{}")
	}
	if c.Result != "" && json.Valid([]byte(c.Result)) {
		out.Result = json.RawMessage(c.Result)
	}
	if c.Cited != "" {
		_ = json.Unmarshal([]byte(c.Cited), &out.Cited)
	}
	if !c.CreatedAt.IsZero() {
		at := c.CreatedAt
		out.Time = &at
	}
	return out
}

// ClientTool is a chat.clientTool contribution the browser declares with a
// message, so the model may call it.
type ClientTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description"`
	Input        json.RawMessage `json:"input,omitempty"` // the arguments' JSON Schema
	Effect       string          `json:"effect"`          // read or ui
	WaitsForUser bool            `json:"waitsForUser,omitempty"`
}

func (t ClientTool) ToTool() tools.Tool {
	out := tools.Tool{Title: t.Title, Description: t.Description, Input: t.Input, Effect: tools.Effect(t.Effect), Location: tools.LocationClient}
	out.ID, out.Source = t.Name, "browser"
	if t.WaitsForUser {
		out.Wait = tools.WaitForUser
	}
	return out
}
