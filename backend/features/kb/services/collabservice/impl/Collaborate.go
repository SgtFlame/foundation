package impl

import (
	"context"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

// Collaborate joins args.Conn to the file's room and relays its messages until
// the connection ends. An unknown FileID (the room was dropped, e.g. after a
// restart) is a 404: the client must open the file again.
func (s *service) Collaborate(args collabservice.CollaborateArgs) collabservice.CollaborateResponse {
	r := s.room(args.FileID)
	if r == nil {
		return collabservice.CollaborateResponse{Common: *args.Fail(http.StatusNotFound, errNotOpen())}
	}
	readOnly, failed := r.access(args.ServiceArgs)
	if failed != nil {
		return collabservice.CollaborateResponse{Common: *failed}
	}

	ctx, cancel := context.WithCancel(args.Context())
	defer cancel()
	c := newClient(args.Conn, cancel)
	c.readOnly, c.args = readOnly, args.ServiceArgs
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
