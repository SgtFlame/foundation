package impl

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// CreateVolume converts the existing folder at args.Path into a volume by
// writing its marker with a new TSID.
func (s *service) CreateVolume(args volumeservice.CreateVolumeArgs) volumeservice.CreateVolumeResponse {
	sa := args.ServiceArgs
	dir, failed := s.resolve(sa, args.Path, true)
	if failed == nil {
		failed = s.statFolder(sa, dir)
	}
	if failed == nil {
		failed = volumeservice.Check(sa, s.root, dir, "write")
	}
	if failed != nil {
		return volumeservice.CreateVolumeResponse{Common: *failed}
	}

	marker := volumeservice.Marker{ID: features.NewTSID(), Created: time.Now().UTC()}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return volumeservice.CreateVolumeResponse{Common: *sa.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}

	// O_EXCL makes converting a folder that is already a volume fail, even
	// when two requests race.
	file, err := os.OpenFile(filepath.Join(dir, volumeservice.MarkerFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case errors.Is(err, fs.ErrExist):
		return volumeservice.CreateVolumeResponse{Common: *sa.Fail(http.StatusConflict, log.Code("already_volume").Errorf("folder %q is already a volume", s.rel(dir)))}
	case err != nil:
		return volumeservice.CreateVolumeResponse{Common: *sa.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
		return volumeservice.CreateVolumeResponse{Common: *sa.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}

	return volumeservice.CreateVolumeResponse{Common: features.OK(), VolumeID: marker.ID}
}
