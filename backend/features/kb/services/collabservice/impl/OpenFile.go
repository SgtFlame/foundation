package impl

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
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
		dir := filepath.Dir(file)
		r = newRoom(s, features.NewTSID(), fileDoc{svc: s, file: file, typ: args.Type}, func(args features.ServiceArgs) (bool, *features.ServiceResponse) {
			if !kbservice.InTenant(args.Principal) {
				return false, args.Fail(authz.Status, kbservice.ErrOtherTenant())
			}
			if f := volumeservice.Check(args, s.root, dir, "read"); f != nil {
				return false, f
			}
			return !volumeservice.May(args, s.root, dir, "write"), nil
		})
		s.rooms[r.id] = r
		s.byPath[file] = r
	}
	return collabservice.OpenFileResponse{Common: features.OK(), FileID: r.id, ReadOnly: readOnly}
}

// OpenDoc returns the room of a document of a contributed kind, opening
// it when nobody has it open.
func (s *service) OpenDoc(args collabservice.OpenDocArgs) collabservice.OpenFileResponse {
	sa := args.ServiceArgs
	var kind collabservice.DocumentKind
	ok := false
	if s.kinds != nil {
		kind, ok = s.kinds(args.Kind)
	}
	if !ok {
		return collabservice.OpenFileResponse{Common: *sa.Fail(http.StatusBadRequest, log.Code("unknown_document_kind").Errorf("there is no document kind %q", args.Kind))}
	}
	d, failed := kind.Open(sa, args.Ref)
	if failed != nil {
		return collabservice.OpenFileResponse{Common: *failed}
	}
	readOnly, failed := d.Access(sa)
	if failed != nil {
		return collabservice.OpenFileResponse{Common: *failed}
	}
	doc := otherDoc{d: d}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byPath[doc.key()]
	if !ok {
		r = newRoom(s, features.NewTSID(), doc, d.Access)
		s.rooms[r.id] = r
		s.byPath[doc.key()] = r
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
