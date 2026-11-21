package gemini

import (
	"errors"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"google.golang.org/genai"
)

// forge 06g: Gemini's 503 (overloaded) and 429 are busy, so the tool loop
// tries them again; a wrong key is 503 too, but not busy
func TestWrapMarksBusy(t *testing.T) {
	for code, want := range map[int]struct {
		status uint16
		busy   bool
	}{503: {503, true}, 429: {429, true}, 401: {503, false}, 500: {502, false}, 400: {400, false}} {
		var e *providers.Error
		if !errors.As(wrap(genai.APIError{Code: code, Message: "x"}), &e) || e.Code != want.status || e.Busy != want.busy {
			t.Errorf("%d: %+v", code, e)
		}
	}
	if errors.Is(providers.Wrap(wrap(genai.APIError{Code: 503})), providers.ErrNotConfigured) {
		t.Error("a busy provider reads as not configured")
	}
}
