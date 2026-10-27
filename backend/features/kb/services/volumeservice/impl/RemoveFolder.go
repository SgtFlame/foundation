package impl

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"

	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// RemoveFolder removes the last folder of args.Path. Only empty folders that
// are not volumes are removed, so no content is ever deleted.
func (s *service) RemoveFolder(args volumeservice.RemoveFolderArgs) volumeservice.RemoveFolderResponse {
	sa := args.ServiceArgs
	dir, failed := s.resolve(sa, args.Path, true)
	if failed == nil {
		failed = s.statFolder(sa, dir)
	}
	if failed == nil {
		failed = volumeservice.Check(sa, s.root, filepath.Dir(dir), "write")
	}
	if failed == nil && isVolume(dir) {
		failed = sa.Fail(http.StatusConflict, log.Code("folder_is_volume").Errorf("folder %q is a volume", s.rel(dir)))
	}
	if failed != nil {
		return volumeservice.RemoveFolderResponse{Common: *failed}
	}

	entries, err := os.ReadDir(dir)
	switch {
	case err != nil:
		failed = sa.Fail(http.StatusInternalServerError, log.Wrap(err))
	case len(entries) > 0:
		failed = sa.Fail(http.StatusConflict, log.Code("folder_not_empty").Errorf("folder %q is not empty", s.rel(dir)))
	default:
		if err := os.Remove(dir); err != nil {
			failed = sa.Fail(http.StatusInternalServerError, log.Wrap(err))
		}
	}
	if failed != nil {
		return volumeservice.RemoveFolderResponse{Common: *failed}
	}
	return volumeservice.RemoveFolderResponse{Common: features.OK()}
}
