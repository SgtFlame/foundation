package impl

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// CreateFile creates an empty file of args.Type at args.Path. Files can only be
// created inside a volume, in a folder that exists.
func (s *service) CreateFile(args volumeservice.CreateFileArgs) volumeservice.CreateFileResponse {
	if !slices.Contains(volumeservice.FileTypes, args.Type) {
		return volumeservice.CreateFileResponse{Common: *fail(http.StatusBadRequest, "unsupported file type %q", args.Type)}
	}
	path := args.Path
	path.Type(args.Type)
	file, failed := s.resolve(path, true)
	parent := filepath.Dir(file)
	if failed == nil {
		failed = s.statFolder(parent)
	}
	if failed == nil && !s.insideVolume(parent) {
		failed = fail(http.StatusBadRequest, "folder %q is not inside a volume", s.rel(parent))
	}
	if failed != nil {
		return volumeservice.CreateFileResponse{Common: *failed}
	}

	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case errors.Is(err, fs.ErrExist):
		failed = fail(http.StatusConflict, "%q already exists", s.rel(file))
	case err != nil:
		failed = fail(http.StatusInternalServerError, "%s", err)
	default:
		if err := f.Close(); err != nil {
			failed = fail(http.StatusInternalServerError, "%s", err)
		}
	}
	if failed != nil {
		return volumeservice.CreateFileResponse{Common: *failed}
	}
	return volumeservice.CreateFileResponse{Common: ok()}
}
