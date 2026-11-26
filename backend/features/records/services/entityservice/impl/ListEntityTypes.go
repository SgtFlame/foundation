package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
)

// ListEntityTypes returns each entity type's last published version.
func (s *service) ListEntityTypes(args entityservice.ListEntityTypesArgs) entityservice.ListEntityTypesResponse {
	if failed := s.check(args.ServiceArgs, ""); failed != nil {
		return entityservice.ListEntityTypesResponse{Common: *failed}
	}
	var rows []records.EntityTypeVersion
	last := s.versions(args.ServiceArgs).Select("entity_id, MAX(version)").Group("entity_id")
	err := s.versions(args.ServiceArgs).Where("(entity_id, version) IN (?)", last).Order("name, entity_id").Find(&rows).Error
	if err != nil {
		return entityservice.ListEntityTypesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	out := entityservice.ListEntityTypesResponse{Common: features.OK(), EntityTypes: []entityservice.VersionInfo{}}
	for _, r := range rows {
		out.EntityTypes = append(out.EntityTypes, infoOf(r))
	}
	return out
}
