package prefabdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

type ListPrefabsResponse struct {
	rest.Status
	Prefabs []VersionInfo `json:"prefabs"`
}

// ListPrefabsResponseFrom converts the service's ListPrefabsResponse.
func ListPrefabsResponseFrom(resp prefabservice.ListPrefabsResponse) ListPrefabsResponse {
	return ListPrefabsResponse{Status: rest.StatusFrom(resp.Common), Prefabs: versionsFrom(resp.Prefabs)}
}
