package impl

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

func (s *service) OpenVolumeAsync(args kbservice.OpenVolumeArgs) *kbservice.OpenVolumeRequest {
	req := &kbservice.OpenVolumeRequest{Response: make(chan *kbservice.OpenVolumeResponse, 1)}
	go func() {
		resp := s.OpenVolume(args)
		req.Response <- &resp
	}()
	return req
}

// OpenVolume resolves args.Path under the service root and returns the ID from
// the volume marker there.
func (s *service) OpenVolume(args kbservice.OpenVolumeArgs) kbservice.OpenVolumeResponse {
	path := args.Path.Root(s.root)
	dir, err := path.Build()
	if err != nil {
		return openVolumeError(http.StatusBadRequest, err.Error())
	}

	marker, err := volumeservice.ReadMarker(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return openVolumeError(http.StatusNotFound, fmt.Sprintf("volume %q does not exist", s.rel(dir)))
	case err != nil:
		return openVolumeError(http.StatusInternalServerError, err.Error())
	}

	return kbservice.OpenVolumeResponse{
		Common:   features.ServiceResponse{Code: http.StatusOK},
		VolumeID: marker.ID,
	}
}

func openVolumeError(code uint16, message string) kbservice.OpenVolumeResponse {
	return kbservice.OpenVolumeResponse{
		Common: features.ServiceResponse{Code: code, Message: message},
	}
}

// rel names dir relative to the root, so that messages do not expose server paths.
func (s *service) rel(dir string) string {
	rel, err := filepath.Rel(s.root, dir)
	if err != nil {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}
