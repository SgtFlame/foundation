package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

// ListPrefabs returns each prefab's last published version.
func (s *service) ListPrefabs(args prefabservice.ListPrefabsArgs) prefabservice.ListPrefabsResponse {
	if failed := s.check(args.ServiceArgs, ""); failed != nil {
		return prefabservice.ListPrefabsResponse{Common: *failed}
	}
	var rows []ui.PrefabRow
	err := s.deps.DB.Model(&ui.PrefabRow{}).Scopes(db.InTenant(args.Principal.TenantID)).Where("version > 0").Order("name, prefab_id").Find(&rows).Error
	if err != nil {
		return prefabservice.ListPrefabsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	out := prefabservice.ListPrefabsResponse{Common: features.OK(), Prefabs: []prefabservice.VersionInfo{}}
	for _, r := range rows {
		out.Prefabs = append(out.Prefabs, prefabservice.VersionInfo{PrefabID: r.PrefabID, Version: r.Version, Name: r.Name, Owner: r.Owner, PublishedBy: r.PublishedBy, PublishedAt: r.PublishedAt})
	}
	return out
}
