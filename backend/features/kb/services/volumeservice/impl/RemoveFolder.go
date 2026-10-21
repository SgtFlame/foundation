package impl

import (
	"net/http"
	"os"

	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// RemoveFolder removes the last folder of args.Path. Only empty folders that
// are not volumes are removed, so no content is ever deleted.
func (s *service) RemoveFolder(args volumeservice.RemoveFolderArgs) volumeservice.RemoveFolderResponse {
	dir, failed := s.resolve(args.Path, true)
	if failed == nil {
		failed = s.statFolder(dir)
	}
	if failed == nil && isVolume(dir) {
		failed = fail(http.StatusConflict, "folder %q is a volume", s.rel(dir))
	}
	if failed != nil {
		return volumeservice.RemoveFolderResponse{Common: *failed}
	}

	entries, err := os.ReadDir(dir)
	switch {
	case err != nil:
		failed = fail(http.StatusInternalServerError, "%s", err)
	case len(entries) > 0:
		failed = fail(http.StatusConflict, "folder %q is not empty", s.rel(dir))
	default:
		if err := os.Remove(dir); err != nil {
			failed = fail(http.StatusInternalServerError, "%s", err)
		}
	}
	if failed != nil {
		return volumeservice.RemoveFolderResponse{Common: *failed}
	}
	return volumeservice.RemoveFolderResponse{Common: ok()}
}
