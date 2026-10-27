package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// List returns every entry of the principal's, proposed ones included, and
// their links, then the active entries shared with them.
func (s *service) List(args chronicleservice.ListArgs) chronicleservice.ListResponse {
	if failed := check(args.ServiceArgs, "chronicle.read", 0); failed != nil {
		return chronicleservice.ListResponse{Common: *failed}
	}
	resp := chronicleservice.ListResponse{Common: features.OK()}
	if err := s.owned(args.ServiceArgs).Order("type, title, id").Find(&resp.Entries).Error; err != nil {
		return chronicleservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	var shared []chronicle.Entry
	err := chronicle.Visible(s.db.WithContext(args.Context()), args.Principal).
		Where("NOT (tenant_id = ? AND owner_id = ?) AND status = ?", args.Principal.TenantID, args.Principal.UserID, chronicle.StatusActive).
		Order("type, title, id").Find(&shared).Error
	if err != nil {
		return chronicleservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	resp.Entries = append(resp.Entries, shared...)
	if err := s.owned(args.ServiceArgs).Order("id").Find(&resp.Links).Error; err != nil {
		return chronicleservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return resp
}
