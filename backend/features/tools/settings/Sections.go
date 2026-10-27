// Package toolssettings declares the tools section
// (documentation/plans/Tools.md): the tool loop's limits, and the tools
// turned off for everyone.
package toolssettings

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ID is the tools section's ID.
const ID = "tools"

// Tools is the tools section.
type Tools struct {
	MaxSteps             int      `json:"maxSteps"`             // model calls with tool calls per round; then it must answer
	MaxResultKB          int      `json:"maxResultKB"`          // a result larger than this is cut, with a note to the model
	TimeoutSeconds       int      `json:"timeoutSeconds"`       // a server tool's run
	ClientTimeoutSeconds int      `json:"clientTimeoutSeconds"` // a client tool's result, except those that wait for the user
	Disabled             []string `json:"disabled"`             // tool names never offered
}

// Defaults are the section's defaults, also used when it cannot be read.
var Defaults = Tools{MaxSteps: 8, MaxResultKB: 32, TimeoutSeconds: 30, ClientTimeoutSeconds: 30, Disabled: []string{}}

// Timeout is a server tool's time limit.
func (t Tools) Timeout() time.Duration {
	return seconds(t.TimeoutSeconds, Defaults.TimeoutSeconds)
}

// ClientTimeout is how long a client tool's result is waited for.
func (t Tools) ClientTimeout() time.Duration {
	return seconds(t.ClientTimeoutSeconds, Defaults.ClientTimeoutSeconds)
}

func seconds(n, fallback int) time.Duration {
	if n <= 0 {
		n = fallback
	}
	return time.Duration(n) * time.Second
}

// Contribute registers the tools section.
func Contribute(reg *extensions.Registry) {
	defaults, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 160, Source: "tools"},
		Title:       "Tools",
		Description: "How the model calls tools in a chat round or an Agent node: how many steps it may take, how large a result it sees, and how long a tool may run. Tools that change your data always ask first.",
		Category:    []string{"AI", "Tools"},
		Scopes:      []string{"app", "tenant"},
		Schema:      json.RawMessage(schema),
		Defaults:    defaults,
		Version:     1,
	})
}

const schema = `{
  "type": "object",
  "properties": {
    "maxSteps": {"type": "integer", "title": "Steps per round", "min": 1, "max": 50, "description": "How many times the model may call tools before it must answer"},
    "maxResultKB": {"type": "integer", "title": "Largest result", "unit": "KB", "min": 1, "max": 1024, "description": "A tool result larger than this is cut short, and the model is told so"},
    "timeoutSeconds": {"type": "integer", "title": "Tool time limit", "unit": "seconds", "min": 1, "max": 600},
    "clientTimeoutSeconds": {"type": "integer", "title": "Browser tool time limit", "unit": "seconds", "min": 1, "max": 600, "description": "How long a tool that runs in the browser may take; asking you a question waits until you answer"},
    "disabled": {"type": "array", "title": "Turned off", "description": "Tool names never offered to the model, e.g. kb.edit", "items": {"type": "string"}}
  }
}`
