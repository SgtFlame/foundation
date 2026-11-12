package livehandlers

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// Serve handles GET /api/v1/live, upgrading it to the live connection's
// WebSocket. The route needs a session (rest.RequireUser), so a browser
// whose session expired gets 401 session_expired, signs in again and
// reconnects.
func (lh *LiveHandler) Serve(c *gin.Context) {
	ws, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return // Accept has already responded
	}
	ws.SetReadLimit(16 << 20)
	args := rest.Args(c)
	// The request's context ends with the upgrade's handler, so the
	// connection keeps its values but not its cancellation
	args.Ctx = context.WithoutCancel(args.Context())
	ctx, cancel := context.WithCancel(args.Ctx)
	defer cancel()
	args.Ctx = ctx
	resp := lh.service.Serve(liveservice.ServeArgs{ServiceArgs: args, Socket: socket{ws}})
	if resp.Common.Code >= 300 {
		ws.Close(websocket.StatusCode(4000+int(resp.Common.Code)), resp.Common.Message)
		return
	}
	ws.Close(websocket.StatusNormalClosure, "")
}

// socket adapts a WebSocket to liveservice.Socket.
type socket struct {
	ws *websocket.Conn
}

func (s socket) Read(ctx context.Context) (bool, []byte, error) {
	typ, msg, err := s.ws.Read(ctx)
	return typ == websocket.MessageBinary, msg, err
}

func (s socket) Write(ctx context.Context, binary bool, msg []byte) error {
	typ := websocket.MessageText
	if binary {
		typ = websocket.MessageBinary
	}
	return s.ws.Write(ctx, typ, msg)
}
