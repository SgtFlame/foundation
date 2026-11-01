package uihandlers

import (
	"encoding/json"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/uidto"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
)

// ContributeLive handles the ui.* frames on every live connection, given
// the service once made. A connection's sessions end with it; their
// records stay, so the browser resumes them when it reconnects.
func ContributeLive(reg *extensions.Registry, service func() uiservice.Service) {
	live.Extensions.Register(reg, live.Extension{
		Descriptor: extensions.Descriptor{ID: "ui", Source: "ui"},
		Frames:     []string{"ui.open", "ui.event", "ui.close"},
		Attach: func(c live.Conn) live.ConnHandler {
			return &uiConn{c: c, service: service(), sessions: map[string]*ui.Session{}}
		},
	})
}

type uiConn struct {
	c        live.Conn
	service  uiservice.Service
	mu       sync.Mutex
	sessions map[string]*ui.Session
}

func (u *uiConn) args(trace string) features.ServiceArgs {
	args := u.c.Args()
	if trace != "" {
		args.Ctx = logging.WithContext(args.Context(), logging.FromContext(args.Context()).Trace(trace))
	}
	return args
}

func (u *uiConn) Handle(frameType string, raw json.RawMessage) {
	switch frameType {
	case "ui.open":
		var f uidto.OpenFrame
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		// Opening waits for the page's queries, so it does not hold up the
		// connection's other frames
		go func() {
			resp := u.service.Open(uiservice.OpenArgs{ServiceArgs: u.args(f.Trace), Session: f.Session, Page: f.Page, Params: f.Params, Send: u.c.Send})
			if resp.Common.Code >= 300 {
				u.c.Send(ui.ErrorFrame{Type: "ui.error", Session: f.Session, Code: resp.Common.Code, Message: resp.Common.Message, ErrorCode: resp.Common.ErrorCode})
				return
			}
			u.mu.Lock()
			u.sessions[f.Session] = resp.Session
			u.mu.Unlock()
		}()
	case "ui.event":
		var f uidto.EventFrame
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		if s := u.session(f.Session); s != nil {
			u.service.Event(uiservice.EventArgs{ServiceArgs: u.args(f.Trace), Session: s, Handler: f.Handler, Path: f.Path, Payload: f.Payload})
		} else {
			u.c.Send(ui.ErrorFrame{Type: "ui.error", Session: f.Session, Code: 404, Message: "this session is not open", ErrorCode: "session_not_open"})
		}
	case "ui.close":
		var f uidto.CloseFrame
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		u.mu.Lock()
		s := u.sessions[f.Session]
		delete(u.sessions, f.Session)
		u.mu.Unlock()
		if s != nil {
			u.service.Close(uiservice.CloseArgs{ServiceArgs: u.c.Args(), Session: s, Forget: true})
		}
	}
}

func (u *uiConn) session(id string) *ui.Session {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sessions[id]
}

func (u *uiConn) Close() {
	u.mu.Lock()
	open := u.sessions
	u.sessions = map[string]*ui.Session{}
	u.mu.Unlock()
	for _, s := range open {
		u.service.Close(uiservice.CloseArgs{ServiceArgs: u.c.Args(), Session: s})
	}
}
