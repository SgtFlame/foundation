package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type service struct {
	root string
}

// The path helpers are shared with other services in volumeservice/Files.go.

func (s *service) resolve(path kbservice.VolumePath, requireFolder bool) (string, *features.ServiceResponse) {
	return volumeservice.Resolve(s.root, path, requireFolder)
}

func (s *service) statFolder(dir string) *features.ServiceResponse {
	return volumeservice.StatFolder(s.root, dir)
}

func (s *service) insideVolume(dir string) bool {
	return volumeservice.InsideVolume(s.root, dir)
}

func (s *service) rel(dir string) string {
	return volumeservice.Rel(s.root, dir)
}

var (
	fail     = volumeservice.Fail
	ok       = volumeservice.OK
	isVolume = volumeservice.IsVolume
)
