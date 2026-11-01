package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
)

// ListVersions returns an entity type's published versions, newest first.
func (s *service) ListVersions(args entityservice.ListVersionsArgs) entityservice.ListVersionsResponse {
	if failed := s.check(args.ServiceArgs, args.ID); failed != nil {
		return entityservice.ListVersionsResponse{Common: *failed}
	}
	var rows []records.EntityTypeVersion
	if err := s.versions(args.ServiceArgs).Where("entity_id = ?", args.ID).Order("version DESC").Find(&rows).Error; err != nil {
		return entityservice.ListVersionsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	out := entityservice.ListVersionsResponse{Common: features.OK(), Versions: []entityservice.VersionInfo{}}
	for _, r := range rows {
		out.Versions = append(out.Versions, infoOf(r))
	}
	return out
}
