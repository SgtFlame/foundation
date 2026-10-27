package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
)

// List pages through the principal's conversations, newest first.
func (s *service) List(args conversationservice.ListArgs) conversationservice.ListResponse {
	if failed := check(args.ServiceArgs, "conversation.read", 0); failed != nil {
		return conversationservice.ListResponse{Common: *failed}
	}
	limit := args.Limit
	switch {
	case limit <= 0:
		limit = 5
	case limit > 50:
		limit = 50
	}
	q := s.owned(args.ServiceArgs)
	if len(args.IDs) > 0 {
		q = q.Where("id IN ?", args.IDs)
	}
	if args.Before != 0 {
		q = q.Where("id < ?", args.Before)
	}
	var list []conversation.Conversation
	if err := q.Order("id DESC").Limit(limit + 1).Find(&list).Error; err != nil {
		return conversationservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	resp := conversationservice.ListResponse{Common: features.OK(), Conversations: list, More: len(list) > limit}
	if resp.More {
		resp.Conversations = list[:limit]
	}
	return resp
}
