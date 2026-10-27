package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/index/rest/indexdto"
)

type ListRequest struct {
	Before uint64         `json:"before,string,omitempty"` // only conversations older than this one
	Limit  int            `json:"limit,omitempty"`         // 5 by default, at most 50
	IDs    indexdto.TSIDs `json:"ids,omitempty"`           // only these conversations
}

// ToArgs converts the request to the service's ListArgs.
func (r ListRequest) ToArgs(args features.ServiceArgs) conversationservice.ListArgs {
	return conversationservice.ListArgs{ServiceArgs: args, Before: r.Before, Limit: r.Limit, IDs: r.IDs}
}
