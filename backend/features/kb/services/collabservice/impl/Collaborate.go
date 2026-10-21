package impl

import (
	"context"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// Collaborate joins args.Conn to the file's room and relays its messages until
// the connection ends. An unknown FileID (the room was dropped, e.g. after a
// restart) is a 404: the client must open the file again.
func (s *service) Collaborate(args collabservice.CollaborateArgs) collabservice.CollaborateResponse {
	r := s.room(args.FileID)
	if r == nil {
		return collabservice.CollaborateResponse{Common: *volumeservice.Fail(http.StatusNotFound, "file is no longer open")}
	}

	ctx, cancel := context.WithCancel(args.Ctx)
	defer cancel()
	c := newClient(args.Conn, cancel)
	go c.writeLoop(ctx)

	if failed := r.join(c); failed != nil {
		r.leave(c)
		return collabservice.CollaborateResponse{Common: *failed}
	}
	defer r.leave(c)
	if s.room(args.FileID) != r {
		// The room was dropped while we were joining it
		return collabservice.CollaborateResponse{Common: *volumeservice.Fail(http.StatusNotFound, "file is no longer open")}
	}

	for {
		msg, err := args.Conn.Read(ctx)
		if err != nil {
			return collabservice.CollaborateResponse{Common: volumeservice.OK()}
		}
		r.handle(c, msg)
	}
}
