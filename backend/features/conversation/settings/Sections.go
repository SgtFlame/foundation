// Package conversationsettings declares the chat sections
// (documentation/plans/ChatCompletion.md) and the Go types they decode into
// with config.Read.
package conversationsettings

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ID is the chat section's ID.
const ID = "chat"

// Chat is the chat section.
type Chat struct {
	SystemPrompt  string `json:"systemPrompt"`
	HistoryRounds int    `json:"historyRounds"` // the window: earlier rounds sent with a message; 0 sends them all
	TitleLevel    string `json:"titleLevel"`    // the level that names conversations; the default level when unknown
}

// Contribute registers the chat section, and a defaults section for every
// chat.contextProvider, so it must run after the providers have registered.
func Contribute(reg *extensions.Registry) {
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 130, Source: "conversation"},
		Title:       "Chat",
		Description: "How AgentOS › Chat talks to the model: its instructions, how much of the conversation it sends, and the level that names conversations.",
		Category:    []string{"AI", "Chat"},
		Scopes:      []string{"app", "tenant", "user"},
		Schema:      json.RawMessage(chatSchema),
		Defaults:    json.RawMessage(chatDefaults),
		Version:     1,
	})
	for i, p := range conversation.ContextProviders.All(reg) {
		config.Sections.Register(reg, config.Section{
			Descriptor:  extensions.Descriptor{ID: conversation.DefaultsSection(p.ID), Order: 140 + i, Source: p.Source},
			Title:       p.Title,
			Description: "The default for new conversations. Each conversation keeps its own, set in the chat's ⚙ settings.",
			Category:    []string{"AI", "Chat", "Context"},
			Scopes:      []string{"app", "tenant", "user"},
			Schema:      p.Schema,
			Defaults:    p.Defaults,
			Version:     1,
		})
	}
}

const chatSchema = `{
  "type": "object",
  "properties": {
    "systemPrompt": {"type": "string", "title": "System prompt", "format": "multiline", "description": "The instructions every chat round starts with. Context, such as the workspace, is added after them."},
    "historyRounds": {"type": "integer", "title": "Window", "unit": "rounds", "min": 0, "max": 1000, "description": "How many earlier rounds of the conversation are sent with each message, and seen by Chronicle's context matching; 0 sends them all. Older rounds are remembered through Chronicle entries."},
    "titleLevel": {"type": "string", "title": "Title level", "ref": "ai.chat.levels#levels", "description": "The level that writes each conversation's title after its first round"}
  }
}`

const chatDefaults = `{
  "systemPrompt": "You are the assistant in Foundation, a workbench for knowledge work. Answer clearly and concisely, in Markdown. When the user's workspace is described below, use it to work out what \"this\", \"it\" or a name refers to; when more than one thing could be meant, ask which. When you do not know, say so.",
  "historyRounds": 15,
  "titleLevel": "fast"
}`
