package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type service struct {
	root   string
	events volumeservice.FileEvents
}

// The path helpers are shared with other services in volumeservice/Files.go.

func (s *service) resolve(args features.ServiceArgs, path kbservice.VolumePath, requireFolder bool) (string, *features.ServiceResponse) {
	return volumeservice.Resolve(args, s.root, path, requireFolder)
}

func (s *service) statFolder(args features.ServiceArgs, dir string) *features.ServiceResponse {
	return volumeservice.StatFolder(args, s.root, dir)
}

func (s *service) insideVolume(dir string) bool {
	return volumeservice.InsideVolume(s.root, dir)
}

func (s *service) rel(dir string) string {
	return volumeservice.Rel(s.root, dir)
}

var (
	log      = volumeservice.Log.Tags("volume")
	isVolume = volumeservice.IsVolume
)
