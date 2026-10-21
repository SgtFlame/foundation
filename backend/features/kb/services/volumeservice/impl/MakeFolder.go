package impl

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// MakeFolder creates the last folder of args.Path; its parent must exist.
func (s *service) MakeFolder(args volumeservice.MakeFolderArgs) volumeservice.MakeFolderResponse {
	dir, failed := s.resolve(args.Path, true)
	if failed == nil {
		failed = s.statFolder(filepath.Dir(dir))
	}
	if failed != nil {
		return volumeservice.MakeFolderResponse{Common: *failed}
	}

	err := os.Mkdir(dir, 0o755)
	switch {
	case errors.Is(err, fs.ErrExist):
		failed = fail(http.StatusConflict, "folder %q already exists", s.rel(dir))
	case err != nil:
		failed = fail(http.StatusInternalServerError, "%s", err)
	}
	if failed != nil {
		return volumeservice.MakeFolderResponse{Common: *failed}
	}
	return volumeservice.MakeFolderResponse{Common: ok()}
}
