package impl

import (
	"net/http"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

// EditDoc applies args.Edits to a document of a kind (forge 18c), as
// EditFile does to a file: through the room's writer while someone has it
// open, else by loading, changing and saving it as the caller. The edits
// are decided against the document's saved text (collabservice.Prepare),
// so a range names a place in that text.
func (s *service) EditDoc(args collabservice.EditDocArgs) collabservice.EditDocResponse {
	sa := args.ServiceArgs
	fail := func(f *features.ServiceResponse) collabservice.EditDocResponse {
		return collabservice.EditDocResponse{Common: *f}
	}
	if len(args.Edits) == 0 {
		return fail(sa.Fail(http.StatusBadRequest, log.Code("no_edits").New("there are no edits")))
	}
	var kind collabservice.DocumentKind
	ok := false
	if s.kinds != nil {
		kind, ok = s.kinds(args.Kind)
	}
	if !ok {
		return fail(sa.Fail(http.StatusBadRequest, log.Code("unknown_document_kind").Errorf("there is no document kind %q", args.Kind)))
	}
	d, failed := kind.Open(sa, args.Ref)
	if failed != nil {
		return fail(failed)
	}
	readOnly, failed := d.Access(sa)
	if failed != nil {
		return fail(failed)
	}
	if readOnly {
		return fail(sa.Fail(http.StatusForbidden, log.Code("read_only").Errorf("you may read %s but not change it", d.Name())))
	}
	doc := otherDoc{d: d}

	s.mu.Lock()
	r := s.byPath[doc.key()]
	if r != nil && r.empty() {
		delete(s.rooms, r.id)
		delete(s.byPath, doc.key())
		r.drop()
		r = nil
	}
	if r == nil {
		// Holding the lock keeps a room from opening on it meanwhile
		defer s.mu.Unlock()
	} else {
		s.mu.Unlock()
	}

	text, err := d.Load(sa)
	if err != nil {
		return fail(sa.Fail(http.StatusInternalServerError, log.Wrap(err)))
	}
	p := collabservice.Prepare(text, args.Edits)
	out := collabservice.EditDocResponse{Common: features.OK(), Applied: p.Applied, Problems: p.Problems}
	for i, applied := range p.Applied {
		if !applied && !args.Each {
			return fail(sa.Fail(http.StatusConflict, log.Code("edit_failed").Errorf("%s", p.Problems[i])))
		}
	}
	if len(p.Edits) == 0 {
		return out
	}
	if r == nil {
		if err := d.Save(sa, p.Text); err != nil {
			return fail(sa.Fail(http.StatusConflict, log.Code("save_refused").Wrap(err)))
		}
		return out
	}

	id := strconv.FormatUint(features.NewTSID(), 10)
	done, ok := r.sendEdit(id, p.Edits)
	if !ok {
		return fail(sa.Fail(http.StatusConflict, log.Code("doc_opening").Errorf("%s is being opened; try again", d.Name())))
	}
	select {
	case ctl := <-done:
		if ctl.Code != http.StatusOK {
			return fail(sa.Fail(http.StatusConflict, log.Code("edit_failed").Errorf("%s", ctl.Message)))
		}
		out.Live = true
		return out
	case <-time.After(editTimeout):
	case <-sa.Context().Done():
	}
	r.forgetEdit(id)
	return fail(sa.Fail(http.StatusGatewayTimeout, log.Code("edit_timeout").Errorf("the editor that has %s open did not apply the edit", d.Name())))
}
