package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
)

type RoundsRequest struct {
	ConversationID uint64 `json:"conversationId,string"`
}

// ToArgs converts the request to the service's RoundsArgs.
func (r RoundsRequest) ToArgs(args features.ServiceArgs) conversationservice.RoundsArgs {
	return conversationservice.RoundsArgs{ServiceArgs: args, ConversationID: r.ConversationID}
}
