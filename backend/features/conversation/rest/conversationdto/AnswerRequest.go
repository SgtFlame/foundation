package conversationdto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// AnswerRequest answers a waiting tool call: approval for an awaiting one,
// result or error for a client one.
type AnswerRequest struct {
	RoundID  uint64          `json:"roundId,string"`
	CallID   string          `json:"callId"`
	Approval string          `json:"approval,omitempty"` // approved, always or denied
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// ToArgs converts the request to the service's AnswerArgs.
func (r AnswerRequest) ToArgs(args features.ServiceArgs) conversationservice.AnswerArgs {
	return conversationservice.AnswerArgs{ServiceArgs: args, RoundID: r.RoundID, CallID: r.CallID, Approval: tools.Approval(r.Approval), Result: r.Result, Error: r.Error}
}
