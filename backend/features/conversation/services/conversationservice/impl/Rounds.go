package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// Rounds returns every round of one of the principal's conversations,
// oldest first.
func (s *service) Rounds(args conversationservice.RoundsArgs) conversationservice.RoundsResponse {
	if failed := check(args.ServiceArgs, "conversation.read", args.ConversationID); failed != nil {
		return conversationservice.RoundsResponse{Common: *failed}
	}
	if _, failed := s.find(args.ServiceArgs, args.ConversationID); failed != nil {
		return conversationservice.RoundsResponse{Common: *failed}
	}
	var rounds []conversation.Round
	if err := s.owned(args.ServiceArgs).Where("conversation_id = ?", args.ConversationID).Order("id").Find(&rounds).Error; err != nil {
		return conversationservice.RoundsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	resp := conversationservice.RoundsResponse{Common: features.OK(), Rounds: rounds}
	if len(rounds) > 0 {
		ids := make([]uint64, len(rounds))
		for i, r := range rounds {
			ids[i] = r.ID
		}
		err := s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).
			Where("round_id IN ?", ids).Order("round_id, number").Find(&resp.Segments).Error
		if err != nil {
			return conversationservice.RoundsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
		err = s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).
			Where("owner_id = ? AND round_id IN ?", args.Principal.UserID, ids).Order("round_id, step, length(call_id), call_id").Find(&resp.ToolCalls).Error
		if err != nil {
			return conversationservice.RoundsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
	}
	return resp
}
