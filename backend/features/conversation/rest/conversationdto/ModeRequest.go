package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// ModeRequest switches a conversation's mode (forge 15e).
type ModeRequest struct {
	ConversationID uint64 `json:"conversationId,string"`
	Mode           string `json:"mode"` // a chat.modes id
}

// ToArgs converts the request to the service's SetModeArgs.
func (r ModeRequest) ToArgs(args features.ServiceArgs) conversationservice.SetModeArgs {
	return conversationservice.SetModeArgs{ServiceArgs: args, ConversationID: r.ConversationID, Mode: r.Mode}
}

type ModeResponse struct {
	rest.Status
	Conversation *Conversation `json:"conversation,omitempty"`
}

// ModeResponseFrom converts the service's SetModeResponse.
func ModeResponseFrom(resp conversationservice.SetModeResponse) ModeResponse {
	out := ModeResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Conversation.ID != 0 {
		c := ConversationFrom(resp.Conversation)
		out.Conversation = &c
	}
	return out
}
