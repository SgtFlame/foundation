package impl

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"

	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// MakeFolder creates the last folder of args.Path; its parent must exist.
func (s *service) MakeFolder(args volumeservice.MakeFolderArgs) volumeservice.MakeFolderResponse {
	sa := args.ServiceArgs
	dir, failed := s.resolve(sa, args.Path, true)
	if failed == nil {
		failed = s.statFolder(sa, filepath.Dir(dir))
	}
	if failed == nil {
		failed = volumeservice.Check(sa, s.root, filepath.Dir(dir), "write")
	}
	if failed != nil {
		return volumeservice.MakeFolderResponse{Common: *failed}
	}

	err := os.Mkdir(dir, 0o755)
	switch {
	case errors.Is(err, fs.ErrExist):
		failed = sa.Fail(http.StatusConflict, log.Code("folder_exists").Errorf("folder %q already exists", s.rel(dir)))
	case err != nil:
		failed = sa.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if failed != nil {
		return volumeservice.MakeFolderResponse{Common: *failed}
	}
	return volumeservice.MakeFolderResponse{Common: features.OK()}
}
