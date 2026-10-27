package impl

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// Collaborate joins args.Conn to the file's room and relays its messages until
// the connection ends. An unknown FileID (the room was dropped, e.g. after a
// restart) is a 404: the client must open the file again.
func (s *service) Collaborate(args collabservice.CollaborateArgs) collabservice.CollaborateResponse {
	r := s.room(args.FileID)
	if r == nil {
		return collabservice.CollaborateResponse{Common: *args.Fail(http.StatusNotFound, errNotOpen())}
	}
	dir := filepath.Dir(r.file)
	if f := volumeservice.Check(args.ServiceArgs, s.root, dir, "read"); f != nil {
		return collabservice.CollaborateResponse{Common: *f}
	}

	ctx, cancel := context.WithCancel(args.Context())
	defer cancel()
	c := newClient(args.Conn, cancel)
	c.readOnly = !volumeservice.May(args.ServiceArgs, s.root, dir, "write")
	go c.writeLoop(ctx)

	if err := r.join(c); err != nil {
		r.leave(c)
		return collabservice.CollaborateResponse{Common: *args.Fail(http.StatusInternalServerError, err)}
	}
	defer r.leave(c)
	if s.room(args.FileID) != r {
		// The room was dropped while we were joining it
		return collabservice.CollaborateResponse{Common: *args.Fail(http.StatusNotFound, errNotOpen())}
	}

	for {
		msg, err := args.Conn.Read(ctx)
		if err != nil {
			return collabservice.CollaborateResponse{Common: features.OK()}
		}
		r.handle(c, msg)
	}
}

func errNotOpen() error {
	return log.Code("file_not_open").New("file is no longer open")
}
