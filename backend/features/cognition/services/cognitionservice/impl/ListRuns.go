package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// ListRuns pages through the principal's runs of a graph, newest first.
func (s *service) ListRuns(args cognitionservice.ListRunsArgs) cognitionservice.ListRunsResponse {
	if failed := check(args.ServiceArgs, "cognition.read", "runs"); failed != nil {
		return cognitionservice.ListRunsResponse{Common: *failed}
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	q := s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).
		Where("owner_id = ? AND graph_id = ?", args.Principal.UserID, args.GraphID)
	if args.ConversationID != 0 {
		q = q.Where("conversation_id = ?", args.ConversationID)
	}
	if args.Before != 0 {
		q = q.Where("id < ?", args.Before)
	}
	var runs []cognition.Run
	if err := q.Order("id DESC").Limit(limit + 1).Find(&runs).Error; err != nil {
		return cognitionservice.ListRunsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	more := len(runs) > limit
	if more {
		runs = runs[:limit]
	}
	return cognitionservice.ListRunsResponse{Common: features.OK(), Runs: runs, More: more}
}
