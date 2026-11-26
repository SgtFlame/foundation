package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

// ListVersions returns a prefab's published versions, newest first.
func (s *service) ListVersions(args prefabservice.ListVersionsArgs) prefabservice.ListVersionsResponse {
	if failed := s.check(args.ServiceArgs, args.ID); failed != nil {
		return prefabservice.ListVersionsResponse{Common: *failed}
	}
	var rows []ui.PrefabVersion
	if err := s.versions(args.ServiceArgs).Where("prefab_id = ?", args.ID).Order("version DESC").Find(&rows).Error; err != nil {
		return prefabservice.ListVersionsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	out := prefabservice.ListVersionsResponse{Common: features.OK(), Versions: []prefabservice.VersionInfo{}}
	for _, r := range rows {
		out.Versions = append(out.Versions, infoOf(r))
	}
	return out
}
