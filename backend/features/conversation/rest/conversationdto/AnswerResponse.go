package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type AnswerResponse struct {
	rest.Status
}

// AnswerResponseFrom converts the service's AnswerResponse; its code is
// carried by the HTTP status rather than the body.
func AnswerResponseFrom(resp conversationservice.AnswerResponse) AnswerResponse {
	return AnswerResponse{Status: rest.StatusFrom(resp.Common)}
}
