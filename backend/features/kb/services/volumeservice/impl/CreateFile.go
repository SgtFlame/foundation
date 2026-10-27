package impl

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// CreateFile creates a file of args.Type at args.Path holding args.Content. Files can only be
// created inside a volume, in a folder that exists.
func (s *service) CreateFile(args volumeservice.CreateFileArgs) volumeservice.CreateFileResponse {
	sa := args.ServiceArgs
	if !slices.Contains(volumeservice.FileTypes, args.Type) {
		return volumeservice.CreateFileResponse{Common: *sa.Fail(http.StatusBadRequest, log.Code("unsupported_file_type").Errorf("unsupported file type %q", args.Type))}
	}
	path := args.Path
	path.Type(args.Type)
	file, failed := s.resolve(sa, path, true)
	parent := filepath.Dir(file)
	if failed == nil {
		failed = s.statFolder(sa, parent)
	}
	if failed == nil && !s.insideVolume(parent) {
		failed = sa.Fail(http.StatusBadRequest, log.Code("not_in_volume").Errorf("folder %q is not inside a volume", s.rel(parent)))
	}
	if failed == nil {
		failed = volumeservice.Check(sa, s.root, parent, "write")
	}
	if failed != nil {
		return volumeservice.CreateFileResponse{Common: *failed}
	}

	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case errors.Is(err, fs.ErrExist):
		failed = sa.Fail(http.StatusConflict, log.Code("file_exists").Errorf("%q already exists", s.rel(file)))
	case err != nil:
		failed = sa.Fail(http.StatusInternalServerError, log.Wrap(err))
	default:
		_, err = f.WriteString(args.Content)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			failed = sa.Fail(http.StatusInternalServerError, log.Wrap(err))
		}
	}
	if failed != nil {
		return volumeservice.CreateFileResponse{Common: *failed}
	}
	s.events.Publish(volumeservice.FileEvent{Op: volumeservice.FileCreated, File: file})
	return volumeservice.CreateFileResponse{Common: features.OK()}
}
