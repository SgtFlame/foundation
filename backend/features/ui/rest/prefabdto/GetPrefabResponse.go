package prefabdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

// GetPrefabResponse carries the definition as its own JSON document
// (ui.Prefab), the format it is published, exported and imported in.
type GetPrefabResponse struct {
	rest.Status
	Info   *VersionInfo `json:"info,omitempty"`
	Prefab *ui.Prefab   `json:"prefab,omitempty"`
}

// GetPrefabResponseFrom converts the service's GetPrefabResponse.
func GetPrefabResponseFrom(resp prefabservice.GetPrefabResponse) GetPrefabResponse {
	out := GetPrefabResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Prefab.ID == "" {
		return out
	}
	info, p := VersionInfoFrom(resp.Info), resp.Prefab
	out.Info, out.Prefab = &info, &p
	return out
}
