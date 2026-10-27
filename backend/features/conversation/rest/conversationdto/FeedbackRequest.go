package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

type FeedbackRequest struct {
	RoundID  uint64 `json:"roundId,string"`
	Feedback string `json:"feedback"` // up, down, ignore, or "" for neutral
}

// ToArgs converts the request to the service's FeedbackArgs.
func (r FeedbackRequest) ToArgs(args features.ServiceArgs) conversationservice.FeedbackArgs {
	return conversationservice.FeedbackArgs{ServiceArgs: args, RoundID: r.RoundID, Feedback: retrieval.Feedback(r.Feedback)}
}
