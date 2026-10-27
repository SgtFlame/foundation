package cognitiondto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
)

type ListRunsRequest struct {
	GraphID        string `json:"graphId"`
	ConversationID uint64 `json:"conversationId,string,omitempty"`
	Before         uint64 `json:"before,string,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

// ToArgs converts the request to the service's ListRunsArgs.
func (r ListRunsRequest) ToArgs(args features.ServiceArgs) cognitionservice.ListRunsArgs {
	return cognitionservice.ListRunsArgs{ServiceArgs: args, GraphID: r.GraphID, ConversationID: r.ConversationID, Before: r.Before, Limit: r.Limit}
}
