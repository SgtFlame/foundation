package prefabdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

// VersionInfo is a published version of a prefab without its definition.
type VersionInfo struct {
	PrefabID    string    `json:"prefabId"`
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	Owner       string    `json:"owner,omitempty"`
	PublishedBy uint64    `json:"publishedBy,string"`
	PublishedAt time.Time `json:"publishedAt"`
}

// VersionInfoFrom converts the service's VersionInfo.
func VersionInfoFrom(v prefabservice.VersionInfo) VersionInfo {
	return VersionInfo{PrefabID: v.PrefabID, Version: v.Version, Name: v.Name, Owner: v.Owner, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt}
}

func versionsFrom(list []prefabservice.VersionInfo) []VersionInfo {
	out := make([]VersionInfo, len(list))
	for i, v := range list {
		out[i] = VersionInfoFrom(v)
	}
	return out
}
