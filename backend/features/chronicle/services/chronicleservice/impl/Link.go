package impl

import (
	"net/http"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// Link relates two of the principal's entries, or unlinks them. A related
// link has no direction, so linking B to A when A is linked to B changes
// nothing; a typed link (forge 35f-6, "sister of") reads from FromID to
// ToID and is kept once per kind. Both entries' facts are written again.
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
	kind := strings.TrimSpace(args.Kind)
	if kind == "" {
		kind = chronicle.LinkRelated
	}
	if len(kind) > 60 || strings.ContainsAny(kind, "\n\r") {
		return chronicleservice.LinkResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("invalid_link").New("a link's kind is one short line"))}
	}
	between := s.owned(args.ServiceArgs).Where("((from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)) AND (kind = ? OR kind = '')", args.FromID, args.ToID, args.ToID, args.FromID, kind)
	switch {
	case args.Remove && args.Kind == "":
		between = s.owned(args.ServiceArgs).Where("(from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)", args.FromID, args.ToID, args.ToID, args.FromID)
	case kind != chronicle.LinkRelated:
		between = s.owned(args.ServiceArgs).Where("from_id = ? AND to_id = ? AND kind = ?", args.FromID, args.ToID, kind)
	}
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
			link := chronicle.EntryLink{Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal), FromID: args.FromID, ToID: args.ToID, Kind: kind}
			if err := s.db.WithContext(args.Context()).Create(&link).Error; err != nil {
				return fail(err)
			}
		}
	}
	s.refresh(args.ServiceArgs, args.FromID, args.ToID)
	resp := chronicleservice.LinkResponse{Common: features.OK()}
	if err := s.owned(args.ServiceArgs).Where("from_id = ? OR to_id = ?", args.FromID, args.FromID).Order("id").Find(&resp.Links).Error; err != nil {
		return fail(err)
	}
	return resp
}
