// Package aisettings declares the ai feature's settings sections
// (documentation/Configuration.md › First sections) and the Go types
// they decode into with config.Read.
package aisettings

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Section IDs.
const (
	ProvidersID = "ai.providers"
	ModelsID    = "ai.models"
	LevelsID    = "ai.chat.levels"
	StagesID    = "ai.chat.stages"
)

// Contribute registers the ai sections on config.Sections.
func Contribute(reg *extensions.Registry) {
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ProvidersID, Order: 100, Source: "ai"},
		Title:       "Providers",
		Description: "Credentials for the AI providers. A key set for your tenant or for you is used for your calls instead of the server's. A key given by flag or environment variable wins over every stored one.",
		Category:    []string{"AI"},
		Scopes:      []string{"app", "tenant", "user"},
		Schema:      json.RawMessage(providersSchema),
		Defaults:    json.RawMessage(`{}`),
		Version:     1,
	})
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ModelsID, Order: 110, Source: "ai"},
		Title:       "Models",
		Description: "The model catalog: every model the levels and the index may use, with its pricing. Cost is computed when a call is recorded, so editing a price never rewrites history.",
		Category:    []string{"AI"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(modelsSchema),
		Defaults:    json.RawMessage(modelsDefaults),
		Version:     1,
	})
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: LevelsID, Order: 120, Source: "ai"},
		Title:       "Chat levels",
		Description: "The levels offered in the chat composer, each a model from the catalog with its generation settings.",
		Category:    []string{"AI", "Chat"},
		Scopes:      []string{"app", "tenant", "user"},
		Schema:      json.RawMessage(levelsSchema),
		Defaults:    json.RawMessage(levelsDefaults),
		Version:     1,
		Check:       checkLevels,
	})
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: StagesID, Order: 125, Source: "ai"},
		Title:       "Internal stages",
		Description: "The level each internal call uses, such as Chronicle's context matching and memory update, so the level chosen in the composer only affects the reply.",
		Category:    []string{"AI", "Chat"},
		Scopes:      []string{"app", "tenant", "user"},
		Schema:      json.RawMessage(stagesSchema),
		Defaults:    json.RawMessage(`{"match": "fast", "memory": "fast"}`),
		Version:     1,
	})
}

// checkLevels requires exactly one default level.
func checkLevels(value map[string]any) []config.FieldError {
	levels, _ := value["levels"].([]any)
	defaults := 0
	for _, l := range levels {
		if obj, ok := l.(map[string]any); ok && obj["default"] == true {
			defaults++
		}
	}
	if defaults != 1 {
		return []config.FieldError{{Path: "levels", Message: fmt.Sprintf("needs exactly one default level, not %d", defaults)}}
	}
	return nil
}

const providersSchema = `{
  "type": "object",
  "properties": {
    "gemini": {
      "type": "object",
      "title": "Google Gemini",
      "properties": {
        "apiKey": {
          "type": "secret",
          "title": "API key",
          "description": "From Google AI Studio. --gemini-api-key or $GEMINI_API_KEY wins over this."
        }
      }
    }
  }
}`

const price = `{"type": "number", "min": 0, "format": "currency", "unit": "per 1M tokens"}`

var modelsSchema = `{
  "type": "object",
  "properties": {
    "models": {
      "type": "array",
      "title": "Models",
      "key": "id",
      "items": {
        "type": "object",
        "required": ["id", "provider", "model", "kind"],
        "properties": {
          "id": {"type": "string", "title": "ID", "description": "The local name levels refer to", "pattern": "^[a-z0-9][a-z0-9.-]*$"},
          "title": {"type": "string", "title": "Title"},
          "provider": {"type": "enum", "title": "Provider", "options": [{"value": "gemini", "label": "Google Gemini"}]},
          "model": {"type": "string", "title": "Provider model", "description": "The provider's own model ID"},
          "kind": {"type": "enum", "title": "Kind", "options": [{"value": "chat", "label": "Chat"}, {"value": "embed", "label": "Embedding"}]},
          "contextWindow": {"type": "integer", "title": "Context window", "min": 0, "unit": "tokens"},
          "pricing": {
            "type": "object",
            "title": "Pricing",
            "properties": {
              "currency": {"type": "enum", "title": "Currency", "options": [{"value": "USD", "label": "US dollar"}]},
              "inputPerMTok": ` + titled(price, "Input") + `,
              "cachedInputPerMTok": ` + titled(price, "Cached input") + `,
              "outputPerMTok": ` + titled(price, "Output (and thinking)") + `,
              "embedPerMTok": ` + titled(price, "Embedding") + `,
              "tiers": {
                "type": "array",
                "title": "Long-prompt tiers",
                "description": "Prices for prompts above a size, such as Gemini Pro's above 200k tokens",
                "key": "aboveInputTokens",
                "items": {
                  "type": "object",
                  "required": ["aboveInputTokens"],
                  "properties": {
                    "aboveInputTokens": {"type": "integer", "title": "Above input tokens", "min": 0},
                    "inputPerMTok": ` + titled(price, "Input") + `,
                    "cachedInputPerMTok": ` + titled(price, "Cached input") + `,
                    "outputPerMTok": ` + titled(price, "Output") + `
                  }
                }
              },
              "effectiveFrom": {"type": "string", "title": "Effective from", "pattern": "^(\\d{4}-\\d{2}-\\d{2})?$", "description": "YYYY-MM-DD"}
            }
          }
        }
      }
    }
  }
}`

// Paid-tier text prices in USD from ai.google.dev/gemini-api/docs/pricing,
// checked 2026-10-22.
const modelsDefaults = `{
  "models": [
    {"id": "gemini-3.5-flash-lite", "title": "Gemini 3.5 Flash-Lite", "provider": "gemini", "model": "gemini-3.5-flash-lite", "kind": "chat", "contextWindow": 1048576,
     "pricing": {"currency": "USD", "inputPerMTok": 0.10, "cachedInputPerMTok": 0.01, "outputPerMTok": 0.40}},
    {"id": "gemini-3.8-flash", "title": "Gemini 3.8 Flash", "provider": "gemini", "model": "gemini-3.8-flash", "kind": "chat", "contextWindow": 1048576,
     "pricing": {"currency": "USD", "inputPerMTok": 0.30, "cachedInputPerMTok": 0.03, "outputPerMTok": 2.50}},
    {"id": "gemini-2.5-pro", "title": "Gemini 2.5 Pro", "provider": "gemini", "model": "gemini-2.5-pro", "kind": "chat", "contextWindow": 1048576,
     "pricing": {"currency": "USD", "inputPerMTok": 1.25, "cachedInputPerMTok": 0.125, "outputPerMTok": 10.00,
       "tiers": [{"aboveInputTokens": 200000, "inputPerMTok": 2.50, "cachedInputPerMTok": 0.25, "outputPerMTok": 15.00}]}},
    {"id": "gemini-embedding-001", "title": "Gemini Embedding", "provider": "gemini", "model": "gemini-embedding-001", "kind": "embed", "contextWindow": 2048,
     "pricing": {"currency": "USD", "embedPerMTok": 0.15}}
  ]
}`

const levelsSchema = `{
  "type": "object",
  "properties": {
    "levels": {
      "type": "array",
      "title": "Levels",
      "key": "id",
      "min": 1,
      "items": {
        "type": "object",
        "required": ["id", "label", "modelRef"],
        "properties": {
          "id": {"type": "string", "title": "ID", "pattern": "^[a-z0-9][a-z0-9-]*$"},
          "label": {"type": "string", "title": "Label"},
          "icon": {"type": "string", "title": "Icon", "description": "An emoji shown on the level's button"},
          "modelRef": {"type": "string", "title": "Model", "ref": "ai.models#models"},
          "temperature": {"type": "number", "title": "Temperature", "min": 0, "max": 2, "step": 0.1},
          "maxOutputTokens": {"type": "integer", "title": "Max output tokens", "min": 1, "unit": "tokens"},
          "thinkingBudget": {"type": "integer", "title": "Thinking budget", "min": -1, "unit": "tokens", "description": "-1 lets the model decide; 0 turns thinking off where the model allows"},
          "default": {"type": "boolean", "title": "Default level"}
        }
      }
    }
  }
}`

const levelsDefaults = `{
  "levels": [
    {"id": "fast", "label": "Fast", "icon": "⚡", "modelRef": "gemini-3.5-flash-lite", "temperature": 1, "maxOutputTokens": 8192, "thinkingBudget": 0, "default": false},
    {"id": "standard", "label": "Standard", "icon": "💡", "modelRef": "gemini-3.8-flash", "temperature": 1, "maxOutputTokens": 16384, "thinkingBudget": -1, "default": true},
    {"id": "deep", "label": "Deep", "icon": "🧪", "modelRef": "gemini-2.5-pro", "temperature": 1, "maxOutputTokens": 32768, "thinkingBudget": -1, "default": false}
  ]
}`

const stagesSchema = `{
  "type": "object",
  "properties": {
    "match": {"type": "string", "title": "Context matching", "ref": "ai.chat.levels#levels", "description": "Chronicle's stage 1: which memory entries a message needs"},
    "memory": {"type": "string", "title": "Memory update", "ref": "ai.chat.levels#levels", "description": "Updates Chronicle entries and proposes new ones after each reply"}
  }
}`

// titled adds a title to a schema written as a JSON object.
func titled(schema, title string) string {
	return `{"title": "` + title + `", ` + schema[1:]
}
