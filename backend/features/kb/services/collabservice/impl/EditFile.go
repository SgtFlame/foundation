package impl

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// editTimeout is how long the writer has to apply an edit.
const editTimeout = 10 * time.Second

// EditFile applies args.Edits through the room's writer while the file is
// open, else to the file. An empty room waiting to be dropped is dropped
// first, so the next open seeds from the edited file.
func (s *service) EditFile(args collabservice.EditFileArgs) collabservice.EditFileResponse {
	sa := args.ServiceArgs
	if !slices.Contains(volumeservice.FileTypes, args.Type) {
		return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusBadRequest, log.Code("unsupported_file_type").Errorf("unsupported file type %q", args.Type))}
	}
	if len(args.Edits) == 0 {
		return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusBadRequest, log.Code("no_edits").New("there are no edits"))}
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
		failed = volumeservice.Check(sa, s.root, filepath.Dir(file), "write")
	}
	if failed != nil {
		return collabservice.EditFileResponse{Common: *failed}
	}

	s.mu.Lock()
	r := s.byPath[file]
	if r != nil && r.empty() {
		delete(s.rooms, r.id)
		delete(s.byPath, file)
		r = nil
	}
	if r == nil {
		// Holding the lock keeps a room from opening on the file meanwhile
		defer s.mu.Unlock()
		return s.editFile(sa, file, args.Edits)
	}
	s.mu.Unlock()

	id := strconv.FormatUint(features.NewTSID(), 10)
	done, ok := r.sendEdit(id, args.Edits)
	if !ok {
		return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusConflict, log.Code("file_opening").Errorf("%q is being opened; try again", s.rel(file)))}
	}
	select {
	case ctl := <-done:
		if ctl.Code != http.StatusOK {
			return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusConflict, log.Code("edit_failed").Errorf("%s", ctl.Message))}
		}
		return collabservice.EditFileResponse{Common: features.OK(), Live: true}
	case <-time.After(editTimeout):
	case <-sa.Context().Done():
	}
	r.forgetEdit(id)
	return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusGatewayTimeout, log.Code("edit_timeout").Errorf("the editor that has %q open did not apply the edit", s.rel(file)))}
}

// editFile applies edits to the file on disk, atomically.
func (s *service) editFile(sa features.ServiceArgs, file string, edits []collabservice.Edit) collabservice.EditFileResponse {
	content, err := os.ReadFile(file)
	if err != nil {
		return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	text, err := collabservice.ApplyEdits(string(content), edits)
	if err != nil {
		return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusConflict, log.Code("edit_failed").Wrap(err))}
	}
	tmp := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+".tmp")
	if err = os.WriteFile(tmp, []byte(text), 0o644); err == nil {
		err = os.Rename(tmp, file)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return collabservice.EditFileResponse{Common: *sa.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	s.events.Publish(volumeservice.FileEvent{Op: volumeservice.FileChanged, File: file})
	return collabservice.EditFileResponse{Common: features.OK()}
}
