package impl

import (
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Answer hands the user's decision, or the browser's result, to the tool
// call a round of the principal's is waiting on.
func (s *service) Answer(args conversationservice.AnswerArgs) conversationservice.AnswerResponse {
	if failed := check(args.ServiceArgs, "conversation.write", 0); failed != nil {
		return conversationservice.AnswerResponse{Common: *failed}
	}
	switch args.Approval {
	case "", tools.ApprovalApproved, tools.ApprovalAlways, tools.ApprovalDenied:
	default:
		return conversationservice.AnswerResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("unknown_approval").Errorf("unknown approval %q", args.Approval))}
	}
	if len(args.Result) > 0 && !json.Valid(args.Result) {
		return conversationservice.AnswerResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_result").New("the result is not valid JSON"))}
	}
	w := s.waitingFor(args.RoundID, args.CallID)
	if w == nil || w.owner.TenantID != args.Principal.TenantID || w.owner.UserID != args.Principal.UserID {
		return conversationservice.AnswerResponse{Common: *args.Fail(http.StatusNotFound, log.Code("call_not_waiting").With("roundId", args.RoundID, "callId", args.CallID).New("that tool call is not waiting for an answer"))}
	}
	select {
	case w.answers <- args:
		return conversationservice.AnswerResponse{Common: features.OK()}
	default:
		return conversationservice.AnswerResponse{Common: *args.Fail(http.StatusConflict, log.Code("call_answered").New("that tool call was already answered"))}
	}
}
