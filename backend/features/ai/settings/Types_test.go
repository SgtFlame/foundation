package aisettings

import "testing"

func TestCostMicros(t *testing.T) {
	pro := Pricing{InputPerMTok: 1.25, CachedInputPerMTok: 0.125, OutputPerMTok: 10,
		Tiers: []Tier{{AboveInputTokens: 200_000, InputPerMTok: 2.5, CachedInputPerMTok: 0.25, OutputPerMTok: 15}}}
	for name, tc := range map[string]struct {
		tokens Tokens
		want   int64
	}{
		"short":    {Tokens{Input: 1000, Cached: 200, Output: 100, Thinking: 50}, 800*1.25 + 200*0.125 + 150*10},
		"long":     {Tokens{Input: 300_000, Output: 1000}, 300_000*2.5 + 1000*15},
		"embed":    {Tokens{Embed: 1_000_000}, 0},
		"no usage": {Tokens{}, 0},
	} {
		if got := pro.CostMicros(tc.tokens); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
	if got := (Pricing{EmbedPerMTok: 0.15}).CostMicros(Tokens{Embed: 1_000_000}); got != 150_000 {
		t.Errorf("embed: %d", got)
	}
}
