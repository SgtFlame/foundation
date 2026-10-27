package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// Shares lists the principal's own requests, or the moderation queue: the
// tenant's pending requests for its moderators, and every tenant's
// tenant-approved requests for global moderators, others' only.
func (s *service) Shares(args chronicleservice.SharesArgs) chronicleservice.SharesResponse {
	q := s.db.WithContext(args.Context()).Order("id DESC")
	if !args.Queue {
		if failed := check(args.ServiceArgs, "chronicle.read", 0); failed != nil {
			return chronicleservice.SharesResponse{Common: *failed}
		}
		q = chronicle.Owned(q, args.Principal)
	} else {
		tenant := mayModerate(args.ServiceArgs)
		global := mayModerateGlobal(args.ServiceArgs)
		switch {
		case !tenant && !global:
			res := authz.Tenant(args.Principal)
			err := authz.Decision{Reason: "not a moderator"}.Err("chronicle.moderate", res)
			return chronicleservice.SharesResponse{Common: *args.Fail(authz.Status, err)}
		case tenant && global:
			q = q.Where("(tenant_id = ? AND status = ?) OR status = ?", args.Principal.TenantID, chronicle.SharePending, chronicle.ShareTenantApproved)
		case tenant:
			q = q.Where("tenant_id = ? AND status = ?", args.Principal.TenantID, chronicle.SharePending)
		default:
			q = q.Where("status = ?", chronicle.ShareTenantApproved)
		}
		q = q.Where("NOT (tenant_id = ? AND owner_id = ?)", args.Principal.TenantID, args.Principal.UserID)
	}
	resp := chronicleservice.SharesResponse{Common: features.OK()}
	if err := q.Limit(500).Find(&resp.Shares).Error; err != nil {
		return chronicleservice.SharesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return resp
}

// mayModerate tells whether the principal moderates their tenant's shares.
func mayModerate(args features.ServiceArgs) bool {
	return authz.Check(args.Context(), args.Principal, "chronicle.moderate", authz.Tenant(args.Principal)).Allowed
}

// mayModerateGlobal tells whether the principal is a global moderator.
func mayModerateGlobal(args features.ServiceArgs) bool {
	return authz.Check(args.Context(), args.Principal, "chronicle.moderateGlobal", authz.OnApp).Allowed
}
