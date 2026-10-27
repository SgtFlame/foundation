package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListResponse struct {
	rest.Status
	Conversations []Conversation `json:"conversations,omitempty"` // newest first
	More          bool           `json:"more,omitempty"`
}

// ListResponseFrom converts the service's ListResponse; its code is carried
// by the HTTP status rather than the body.
func ListResponseFrom(resp conversationservice.ListResponse) ListResponse {
	out := ListResponse{Status: rest.StatusFrom(resp.Common), More: resp.More}
	for _, c := range resp.Conversations {
		out.Conversations = append(out.Conversations, ConversationFrom(c))
	}
	return out
}
