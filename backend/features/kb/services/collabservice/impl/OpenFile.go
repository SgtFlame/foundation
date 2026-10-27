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
// it if nobody has the file open. The file must exist inside a volume the
// principal may read; without volume.write the file opens read-only.
func (s *service) OpenFile(args collabservice.OpenFileArgs) collabservice.OpenFileResponse {
	sa := args.ServiceArgs
	if !slices.Contains(volumeservice.FileTypes, args.Type) {
		return collabservice.OpenFileResponse{Common: *sa.Fail(http.StatusBadRequest, log.Code("unsupported_file_type").Errorf("unsupported file type %q", args.Type))}
	}
	path := args.Path
	path.Type(args.Type)
	file, failed := volumeservice.Resolve(sa, s.root, path, true)
	if failed == nil {
		failed = s.statFile(sa, file)
	}
	if failed == nil && !volumeservice.InsideVolume(s.root, filepath.Dir(file)) {
		failed = sa.Fail(http.StatusBadRequest, log.Code("not_in_volume").Errorf("%q is not inside a volume", s.rel(file)))
	}
	if failed == nil {
		failed = volumeservice.Check(sa, s.root, filepath.Dir(file), "read")
	}
	if failed != nil {
		return collabservice.OpenFileResponse{Common: *failed}
	}
	readOnly := !volumeservice.May(sa, s.root, filepath.Dir(file), "write")

	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byPath[file]
	if !ok {
		r = newRoom(s, features.NewTSID(), file)
		s.rooms[r.id] = r
		s.byPath[file] = r
	}
	return collabservice.OpenFileResponse{Common: features.OK(), FileID: r.id, ReadOnly: readOnly}
}

// statFile checks that file exists and is a regular file.
func (s *service) statFile(sa features.ServiceArgs, file string) *features.ServiceResponse {
	info, err := os.Stat(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return sa.Fail(http.StatusNotFound, log.Code("file_not_found").Errorf("%q does not exist", s.rel(file)))
	case err != nil:
		return sa.Fail(http.StatusInternalServerError, log.Wrap(err))
	case !info.Mode().IsRegular():
		return sa.Fail(http.StatusBadRequest, log.Code("not_a_file").Errorf("%q is not a file", s.rel(file)))
	}
	return nil
}
