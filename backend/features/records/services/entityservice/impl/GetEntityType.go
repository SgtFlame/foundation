package impl

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
	"gorm.io/gorm"
)

// GetEntityType returns one published version of an entity type, the last
// when no version is named.
func (s *service) GetEntityType(args entityservice.GetEntityTypeArgs) entityservice.GetEntityTypeResponse {
	if failed := s.check(args.ServiceArgs, args.ID); failed != nil {
		return entityservice.GetEntityTypeResponse{Common: *failed}
	}
	q := s.versions(args.ServiceArgs).Where("entity_id = ?", args.ID)
	if args.Version > 0 {
		q = q.Where("version = ?", args.Version)
	}
	var row records.EntityTypeVersion
	err := q.Order("version DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entityservice.GetEntityTypeResponse{Common: *args.Fail(http.StatusNotFound,
			log.Code("entity_not_found").With("entityId", args.ID).New("there is no such published entity type"))}
	}
	if err != nil {
		return entityservice.GetEntityTypeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	var e records.EntityType
	if err := json.Unmarshal([]byte(row.Definition), &e); err != nil {
		return entityservice.GetEntityTypeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return entityservice.GetEntityTypeResponse{Common: features.OK(), Info: infoOf(row), EntityType: e}
}
