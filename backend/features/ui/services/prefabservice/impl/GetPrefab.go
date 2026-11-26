package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
	"github.com/samber/oops"
)

// GetPrefab returns one published version of a prefab, the last when no
// version is named.
func (s *service) GetPrefab(args prefabservice.GetPrefabArgs) prefabservice.GetPrefabResponse {
	if failed := s.check(args.ServiceArgs, args.ID); failed != nil {
		return prefabservice.GetPrefabResponse{Common: *failed}
	}
	p, row, err := prefabs.Version(args.ServiceArgs, s.deps, args.ID, args.Version)
	if err != nil {
		status := uint16(http.StatusInternalServerError)
		if o, ok := oops.AsOops(err); ok && o.Code() == "prefab_not_found" {
			status = http.StatusNotFound
		}
		return prefabservice.GetPrefabResponse{Common: *args.Fail(status, err)}
	}
	return prefabservice.GetPrefabResponse{Common: features.OK(), Info: infoOf(row), Prefab: p}
}
