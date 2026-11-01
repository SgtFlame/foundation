package entitydto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
)

// VersionInfo is a published version of an entity type without its
// definition.
type VersionInfo struct {
	EntityID    string    `json:"entityId"`
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	Owner       string    `json:"owner,omitempty"`
	PublishedBy uint64    `json:"publishedBy,string"`
	PublishedAt time.Time `json:"publishedAt"`
}

// VersionInfoFrom converts the service's VersionInfo.
func VersionInfoFrom(v entityservice.VersionInfo) VersionInfo {
	return VersionInfo{EntityID: v.EntityID, Version: v.Version, Name: v.Name, Owner: v.Owner, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt}
}

func versionsFrom(list []entityservice.VersionInfo) []VersionInfo {
	out := make([]VersionInfo, len(list))
	for i, v := range list {
		out[i] = VersionInfoFrom(v)
	}
	return out
}
