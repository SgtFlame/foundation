package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// Create adds an active entry for the principal and indexes it in the
// background.
func (s *service) Create(args chronicleservice.CreateArgs) chronicleservice.EntryResponse {
	if failed := check(args.ServiceArgs, "chronicle.write", 0); failed != nil {
		return chronicleservice.EntryResponse{Common: *failed}
	}
	entry := chronicle.Entry{Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal), Status: chronicle.StatusActive}
	if failed := s.apply(args.ServiceArgs, args.EntryInput, &entry); failed != nil {
		return chronicleservice.EntryResponse{Common: *failed}
	}
	if err := s.db.WithContext(args.Context()).Create(&entry).Error; err != nil {
		return chronicleservice.EntryResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	s.index(args.ServiceArgs, entry.ID)
	return chronicleservice.EntryResponse{Common: features.OK(), Entry: entry}
}
