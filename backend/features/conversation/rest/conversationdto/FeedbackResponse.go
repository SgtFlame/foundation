package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type FeedbackResponse struct {
	rest.Status
	Round *Round `json:"round,omitempty"`
}

// FeedbackResponseFrom converts the service's FeedbackResponse; its code is
// carried by the HTTP status rather than the body.
func FeedbackResponseFrom(resp conversationservice.FeedbackResponse) FeedbackResponse {
	out := FeedbackResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Round.ID != 0 {
		round := RoundFrom(resp.Round)
		out.Round = &round
	}
	return out
}
