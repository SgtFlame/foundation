package impl

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// OpenFile returns the collaboration room for the file at args.Path, creating
// it if nobody has the file open. The file must exist inside a volume.
func (s *service) OpenFile(args collabservice.OpenFileArgs) collabservice.OpenFileResponse {
	if !slices.Contains(volumeservice.FileTypes, args.Type) {
		return collabservice.OpenFileResponse{Common: *volumeservice.Fail(http.StatusBadRequest, "unsupported file type %q", args.Type)}
	}
	path := args.Path
	path.Type(args.Type)
	file, failed := volumeservice.Resolve(s.root, path, true)
	if failed == nil {
		failed = s.statFile(file)
	}
	if failed == nil && !volumeservice.InsideVolume(s.root, filepath.Dir(file)) {
		failed = volumeservice.Fail(http.StatusBadRequest, "%q is not inside a volume", s.rel(file))
	}
	if failed != nil {
		return collabservice.OpenFileResponse{Common: *failed}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byPath[file]
	if !ok {
		r = newRoom(s, features.NewTSID(), file)
		s.rooms[r.id] = r
		s.byPath[file] = r
	}
	return collabservice.OpenFileResponse{Common: volumeservice.OK(), FileID: r.id}
}

// statFile checks that file exists and is a regular file.
func (s *service) statFile(file string) *features.ServiceResponse {
	info, err := os.Stat(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return volumeservice.Fail(http.StatusNotFound, "%q does not exist", s.rel(file))
	case err != nil:
		return volumeservice.Fail(http.StatusInternalServerError, "%s", err)
	case !info.Mode().IsRegular():
		return volumeservice.Fail(http.StatusBadRequest, "%q is not a file", s.rel(file))
	}
	return nil
}
