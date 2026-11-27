package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// SaveToolCall keeps a tool call made by a loop in any process.
func (s *service) SaveToolCall(args conversationservice.SaveToolCallArgs) conversationservice.SaveToolCallResponse {
	if failed := check(args.ServiceArgs, "conversation.write", 0); failed != nil {
		return conversationservice.SaveToolCallResponse{Common: *failed}
	}
	rec := args.Call
	if rec.ID == 0 {
		return conversationservice.SaveToolCallResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("call_without_id").New("a tool call is saved with its id"))}
	}
	rec.Tenanted, rec.Owned = db.TenantOf(args.Principal), db.OwnedBy(args.Principal)
	if err := conversation.CallsIn(s.db).SaveCall(args.ServiceArgs, &rec); err != nil {
		return conversationservice.SaveToolCallResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return conversationservice.SaveToolCallResponse{Common: features.OK()}
}
