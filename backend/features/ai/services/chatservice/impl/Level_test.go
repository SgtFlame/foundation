package impl

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
)

// sections is a config.Reader over fixed section values.
type sections map[string]string

func (s sections) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	return json.RawMessage(s[section]), nil
}

var levelSettings = sections{
	aisettings.LevelsID: `{"levels": [
		{"id": "fast", "modelRef": "lite", "temperature": 0.5, "maxOutputTokens": 100, "thinkingBudget": 0},
		{"id": "standard", "modelRef": "flash", "default": true},
		{"id": "broken", "modelRef": "missing"}]}`,
	aisettings.ModelsID: `{"models": [{"id": "lite", "model": "m-lite"}, {"id": "flash", "model": "m-flash"}]}`,
}

func TestLevelsFillTheInput(t *testing.T) {
	p := &fakeProvider{}
	s := newService(WithProvider(p), WithConfig(levelSettings))
	msgs := []chatservice.Message{{Role: chatservice.RoleUser, Content: "hi"}}

	resp := s.Complete(chatservice.CompleteArgs{Level: "fast", Messages: msgs})
	in := p.got
	if resp.Common.Code != http.StatusOK || resp.Level != "fast" || resp.Model != "m-lite" ||
		in.Model != "m-lite" || *in.Temperature != 0.5 || *in.MaxTokens != 100 || *in.ThinkingBudget != 0 {
		t.Fatalf("fast: %+v, input %+v", resp, in)
	}

	// What the caller sets wins over the level
	temp := float32(1.5)
	s.Complete(chatservice.CompleteArgs{Level: "fast", Model: "other", Temperature: &temp, Messages: msgs})
	if p.got.Model != "other" || *p.got.Temperature != 1.5 {
		t.Errorf("overrides: %+v", p.got)
	}

	for level, code := range map[string]uint16{"missing": http.StatusBadRequest, "broken": http.StatusBadRequest} {
		if got := s.Complete(chatservice.CompleteArgs{Level: level, Messages: msgs}).Common.Code; got != code {
			t.Errorf("%s: code %d", level, got)
		}
	}
	if got := newService(WithProvider(p)).Complete(chatservice.CompleteArgs{Level: "fast", Messages: msgs}).Common.Code; got != http.StatusServiceUnavailable {
		t.Errorf("no config: code %d", got)
	}
}
