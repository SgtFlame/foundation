package aisettings

import (
	"math"
	"slices"
)

// Providers is the ai.providers section.
type Providers struct {
	Gemini struct {
		APIKey string `json:"apiKey"`
	} `json:"gemini"`
}

// Models is the ai.models section: the model catalog.
type Models struct {
	Models []Model `json:"models"`
}

type Model struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Provider      string  `json:"provider"`
	Model         string  `json:"model"` // the provider's model ID
	Kind          string  `json:"kind"`  // chat or embed
	ContextWindow int     `json:"contextWindow"`
	Pricing       Pricing `json:"pricing"`
}

// Pricing is per million tokens in Currency. Thinking tokens bill as output.
type Pricing struct {
	Currency           string  `json:"currency"`
	InputPerMTok       float64 `json:"inputPerMTok"`
	CachedInputPerMTok float64 `json:"cachedInputPerMTok"`
	OutputPerMTok      float64 `json:"outputPerMTok"`
	EmbedPerMTok       float64 `json:"embedPerMTok"`
	Tiers              []Tier  `json:"tiers"`
	EffectiveFrom      string  `json:"effectiveFrom"`
}

// Tier replaces the prices of prompts above AboveInputTokens.
type Tier struct {
	AboveInputTokens   int     `json:"aboveInputTokens"`
	InputPerMTok       float64 `json:"inputPerMTok"`
	CachedInputPerMTok float64 `json:"cachedInputPerMTok"`
	OutputPerMTok      float64 `json:"outputPerMTok"`
}

// Find is the catalog entry for a provider's model.
func (m Models) Find(provider, model string) (Model, bool) {
	i := slices.IndexFunc(m.Models, func(x Model) bool { return x.Provider == provider && x.Model == model })
	if i < 0 {
		return Model{}, false
	}
	return m.Models[i], true
}

// ByID is the catalog entry a level's ModelRef names.
func (m Models) ByID(id string) (Model, bool) {
	i := slices.IndexFunc(m.Models, func(x Model) bool { return x.ID == id })
	if i < 0 {
		return Model{}, false
	}
	return m.Models[i], true
}

// Tokens are what one call used. Input includes Cached, as providers count it.
type Tokens struct {
	Input, Cached, Output, Thinking, Embed int
}

// CostMicros is the cost of t in millionths of the currency. A price per
// million tokens is exactly the price per token in micro-units.
func (p Pricing) CostMicros(t Tokens) int64 {
	input, cached, output := p.InputPerMTok, p.CachedInputPerMTok, p.OutputPerMTok
	tier := -1
	for i, x := range p.Tiers {
		if t.Input > x.AboveInputTokens && (tier < 0 || x.AboveInputTokens > p.Tiers[tier].AboveInputTokens) {
			tier = i
		}
	}
	if tier >= 0 {
		x := p.Tiers[tier]
		input, cached, output = x.InputPerMTok, x.CachedInputPerMTok, x.OutputPerMTok
	}
	if cached == 0 {
		cached = input
	}
	cost := float64(t.Input-t.Cached)*input + float64(t.Cached)*cached +
		float64(t.Output+t.Thinking)*output + float64(t.Embed)*p.EmbedPerMTok
	return int64(math.Round(cost))
}

// Levels is the ai.chat.levels section.
type Levels struct {
	Levels []Level `json:"levels"`
}

type Level struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Icon            string   `json:"icon"`
	ModelRef        string   `json:"modelRef"` // an ai.models ID
	Temperature     *float64 `json:"temperature"`
	MaxOutputTokens int      `json:"maxOutputTokens"`
	ThinkingBudget  *int     `json:"thinkingBudget"`
	Default         bool     `json:"default"`
}

// Stages is the ai.chat.stages section: the level of each internal call, by
// stage. A level that is not found falls back to the default level.
type Stages struct {
	Match  string `json:"match"`  // Chronicle's stage 1
	Memory string `json:"memory"` // Chronicle's memory update
}

// Find is the level with id, or the default level when id is empty.
func (l Levels) Find(id string) (Level, bool) {
	i := slices.IndexFunc(l.Levels, func(x Level) bool { return (id == "" && x.Default) || (id != "" && x.ID == id) })
	if i < 0 {
		return Level{}, false
	}
	return l.Levels[i], true
}
