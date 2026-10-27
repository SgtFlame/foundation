// Package retrievalsettings declares the retrieval sections
// (documentation/plans/ContextRetrieval.md › Configuration sections) and the
// Go types they decode into with config.Read.
package retrievalsettings

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

const (
	ID         = "retrieval"
	FeedbackID = "retrieval.feedback"
	PresetsID  = "chat.context.presets"
)

// Retrieval is the retrieval section: the pipeline's constants.
type Retrieval struct {
	RRFK            int     `json:"rrfK"`
	CandidateFactor int     `json:"candidateFactor"` // candidates per search: max(candidateFactor·TopK, minCandidates)
	MinCandidates   int     `json:"minCandidates"`
	AxisBase        float64 `json:"axisBase"` // B in the Chronicle ↔ Library multipliers B^(±a/5)
	DefaultTopK     int     `json:"defaultTopK"`
	BudgetShare     float64 `json:"budgetShare"`     // of the level's context window
	MaxBudgetTokens int     `json:"maxBudgetTokens"` // caps the budget, whatever the window
}

// Feedback is the retrieval.feedback section.
type Feedback = retrieval.GroundingSettings

// Presets is the chat.context.presets section: named dial positions shown
// as chips above the RAG Search sliders.
type Presets struct {
	Presets []Preset `json:"presets"`
}

type Preset struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	LexicalSemantic  int    `json:"lexicalSemantic"`
	ChronicleLibrary int    `json:"chronicleLibrary"`
	TopK             int    `json:"topK"`
}

// Contribute registers the retrieval sections on config.Sections.
func Contribute(reg *extensions.Registry) {
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 150, Source: "retrieval"},
		Title:       "Retrieval",
		Description: "How context is found for a chat round: vector and keyword rankings fused by Reciprocal Rank Fusion, weighted by the two dials, then grounded by feedback.",
		Category:    []string{"AI", "Retrieval"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(retrievalSchema),
		Defaults:    json.RawMessage(`{"rrfK": 60, "candidateFactor": 5, "minCandidates": 50, "axisBase": 4, "defaultTopK": 8, "budgetShare": 0.5, "maxBudgetTokens": 10000}`),
		Version:     1,
	})
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: FeedbackID, Order: 151, Source: "retrieval"},
		Title:       "Feedback",
		Description: "How 👍 and 👎 on replies reweight the segments they used: ((1 + up) / (1 + down))^γ, kept between the minimum and the maximum.",
		Category:    []string{"AI", "Retrieval"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(feedbackSchema),
		Defaults:    json.RawMessage(`{"gamma": 0.5, "min": 0.25, "max": 4}`),
		Version:     1,
	})
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: PresetsID, Order: 139, Source: "retrieval"},
		Title:       "RAG Search presets",
		Description: "Named positions of the two RAG Search dials, shown as chips in the chat's ⚙ settings.",
		Category:    []string{"AI", "Chat", "Context"},
		Scopes:      []string{"app", "tenant", "user"},
		Schema:      json.RawMessage(presetsSchema),
		Defaults:    json.RawMessage(presetsDefaults),
		Version:     1,
	})
}

const retrievalSchema = `{
  "type": "object",
  "properties": {
    "rrfK": {"type": "integer", "title": "RRF k", "min": 1, "max": 1000, "description": "Damps the head of each ranking; 60 is customary"},
    "candidateFactor": {"type": "integer", "title": "Candidates per segment", "min": 1, "max": 50, "description": "Each search fetches this many candidates per segment injected, and at least the minimum"},
    "minCandidates": {"type": "integer", "title": "Minimum candidates", "min": 1, "max": 1000},
    "axisBase": {"type": "number", "title": "Chronicle ↔ Library base", "min": 1, "max": 100, "description": "At either end of the dial, one side is favoured base² : 1"},
    "defaultTopK": {"type": "integer", "title": "Default segments", "min": 1, "max": 50},
    "budgetShare": {"type": "number", "title": "Context budget", "min": 0.01, "max": 1, "step": 0.01, "description": "The share of the level's context window injected segments may use"},
    "maxBudgetTokens": {"type": "integer", "title": "Largest context budget", "min": 100, "max": 1000000, "unit": "tokens"}
  }
}`

const feedbackSchema = `{
  "type": "object",
  "properties": {
    "gamma": {"type": "number", "title": "γ", "min": 0, "max": 4, "step": 0.05, "description": "How strongly votes move a segment; 0 ignores them"},
    "min": {"type": "number", "title": "Lowest multiplier", "min": 0.01, "max": 1, "step": 0.01},
    "max": {"type": "number", "title": "Highest multiplier", "min": 1, "max": 100, "step": 0.1}
  }
}`

const presetsSchema = `{
  "type": "object",
  "properties": {
    "presets": {"type": "array", "title": "Presets", "items": {
      "type": "object",
      "required": ["id", "title"],
      "properties": {
        "id": {"type": "string", "title": "ID", "pattern": "^[a-z0-9-]+$"},
        "title": {"type": "string", "title": "Title"},
        "lexicalSemantic": {"type": "integer", "title": "Lexical ↔ Semantic", "min": -5, "max": 5},
        "chronicleLibrary": {"type": "integer", "title": "Chronicles ↔ Library", "min": -5, "max": 5},
        "topK": {"type": "integer", "title": "Segments", "min": 1, "max": 50}
      }
    }}
  }
}`

const presetsDefaults = `{"presets": [
  {"id": "code", "title": "Code lookup", "lexicalSemantic": -4, "chronicleLibrary": 3, "topK": 8},
  {"id": "brainstorm", "title": "Brainstorm", "lexicalSemantic": 4, "chronicleLibrary": 0, "topK": 12},
  {"id": "recall", "title": "Recall", "lexicalSemantic": 0, "chronicleLibrary": -4, "topK": 8}
]}`
