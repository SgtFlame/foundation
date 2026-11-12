package tools

import (
	"encoding/json"
	"strings"
)

// Where a Spec's tool comes from.
const (
	SourceServer = "server" // a tools.tool contribution
	SourceClient = "client" // a chat.clientTool the browser declared with the message
	SourceGraph  = "graph"  // a graph tool: a function graph a mode or agent graph declares (forge 06i)
)

// Spec is a tool as a graph's tool set carries it (forge 06i): a server or
// client tool by name, maybe re-described, or a graph tool, which a
// function graph runs. A set is []Spec, the ToolSet pin type.
type Spec struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input,omitempty"`
	Effect      Effect          `json:"effect,omitempty"`
	Source      string          `json:"source"`
	// Graph is a graph tool's function graph, run from its On call with
	// the call's arguments as inputs; its outputs are the result.
	Graph string `json:"graph,omitempty"`
	// Asks is whether a graph tool's call asks first; unset follows its
	// effect.
	Asks *bool `json:"asks,omitempty"`
	// Summary is a graph tool's card line, a template over its outputs.
	Summary        string `json:"summary,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// SpecOf is a server or client tool as a Spec.
func SpecOf(t Tool) Spec {
	source := SourceServer
	if t.Client() {
		source = SourceClient
	}
	return Spec{Name: t.ID, Title: t.Title, Description: t.Description, Input: t.Input, Effect: t.Effect, Source: source}
}

// Matches reports whether a tool name matches one of patterns: a name, or
// a prefix ending in "." (forge.).
func Matches(name string, patterns []string) bool {
	for _, p := range patterns {
		if p == name || (strings.HasSuffix(p, ".") && strings.HasPrefix(name, p)) {
			return true
		}
	}
	return false
}
