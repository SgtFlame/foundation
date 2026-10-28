package gemini

import (
	"testing"

	"google.golang.org/genai"
)

func TestThinking(t *testing.T) {
	cases := []struct {
		model  string
		budget int
		level  genai.ThinkingLevel
		want   int32 // the budget, when there is no level
	}{
		{"gemini-3.5-flash-lite", 0, genai.ThinkingLevelMinimal, 0},
		{"models/gemini-3.8-flash", 0, genai.ThinkingLevelMinimal, 0},
		{"gemini-3-pro", 0, genai.ThinkingLevelLow, 0},
		{"gemini-3.5-flash-lite", -1, "", -1},
		{"gemini-3.8-flash", 1024, "", 1024},
		{"gemini-2.5-flash", 0, "", 0},
	}
	for _, c := range cases {
		got := thinking(c.model, c.budget)
		if got.ThinkingLevel != c.level {
			t.Errorf("%s budget %d: level %q, want %q", c.model, c.budget, got.ThinkingLevel, c.level)
		}
		if c.level == "" && (got.ThinkingBudget == nil || *got.ThinkingBudget != c.want) {
			t.Errorf("%s budget %d: budget %v, want %d", c.model, c.budget, got.ThinkingBudget, c.want)
		}
		if c.level != "" && got.ThinkingBudget != nil {
			t.Errorf("%s budget %d: sends a budget with level %q", c.model, c.budget, c.level)
		}
	}
}
