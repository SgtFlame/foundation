package impl

import (
	"errors"
	"io/fs"
	"net/http"

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
		return kbservice.OpenVolumeResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("invalid_path").Wrap(err))}
	}

	marker, err := volumeservice.ReadMarker(dir)
	if err == nil {
		if f := volumeservice.Check(args.ServiceArgs, s.root, dir, "read"); f != nil {
			return kbservice.OpenVolumeResponse{Common: *f}
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return kbservice.OpenVolumeResponse{Common: *args.Fail(http.StatusNotFound, log.Code("volume_not_found").Errorf("volume %q does not exist", s.rel(dir)))}
	case err != nil:
		return kbservice.OpenVolumeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}

	return kbservice.OpenVolumeResponse{
		Common:   features.OK(),
		VolumeID: marker.ID,
	}
}

var log = volumeservice.Log.Tags("volume")

// rel names dir relative to the root, so that messages do not expose server paths.
func (s *service) rel(dir string) string {
	return volumeservice.Rel(s.root, dir)
}
