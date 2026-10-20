package kbdto

import "github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"

// VolumePath is the wire form of kbservice.VolumePath, without the root, which
// is always supplied by the service.
type VolumePath struct {
	Tenant  string   `json:"tenant,omitempty"`
	User    string   `json:"user,omitempty"`
	Folders []string `json:"folders"`
}

// ToVolumePath converts to a kbservice.VolumePath; invalid segments are
// reported when the service builds the path.
func (vp VolumePath) ToVolumePath() kbservice.VolumePath {
	path := kbservice.NewVolumePath()
	if vp.Tenant != "" {
		path.Tenant(vp.Tenant)
	}
	if vp.User != "" {
		path.User(vp.User)
	}
	path.Folder(vp.Folders...)
	return *path
}
