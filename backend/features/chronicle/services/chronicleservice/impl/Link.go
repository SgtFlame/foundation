package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// Link relates two of the principal's entries, or unlinks them. A link has
// no direction, so linking B to A when A is linked to B changes nothing.
func (s *service) Link(args chronicleservice.LinkArgs) chronicleservice.LinkResponse {
	if failed := check(args.ServiceArgs, "chronicle.write", args.FromID); failed != nil {
		return chronicleservice.LinkResponse{Common: *failed}
	}
	if args.FromID == args.ToID {
		return chronicleservice.LinkResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("invalid_link").New("an entry cannot be linked to itself"))}
	}
	for _, id := range []uint64{args.FromID, args.ToID} {
		if _, failed := s.find(args.ServiceArgs, id); failed != nil {
			return chronicleservice.LinkResponse{Common: *failed}
		}
	}
	fail := func(err error) chronicleservice.LinkResponse {
		return chronicleservice.LinkResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	between := s.owned(args.ServiceArgs).Where("(from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)", args.FromID, args.ToID, args.ToID, args.FromID)
	if args.Remove {
		if err := between.Delete(&chronicle.EntryLink{}).Error; err != nil {
			return fail(err)
		}
	} else {
		var n int64
		if err := between.Model(&chronicle.EntryLink{}).Count(&n).Error; err != nil {
			return fail(err)
		}
		if n == 0 {
			link := chronicle.EntryLink{Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal), FromID: args.FromID, ToID: args.ToID, Kind: chronicle.LinkRelated}
			if err := s.db.WithContext(args.Context()).Create(&link).Error; err != nil {
				return fail(err)
			}
		}
	}
	resp := chronicleservice.LinkResponse{Common: features.OK()}
	if err := s.owned(args.ServiceArgs).Where("from_id = ? OR to_id = ?", args.FromID, args.FromID).Order("id").Find(&resp.Links).Error; err != nil {
		return fail(err)
	}
	return resp
}
