package collabhandlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// Collaborate handles GET /api/v1/kb/collab/files/:fileId, upgrading it to a
// WebSocket that speaks the y-websocket protocol. A service failure closes the
// socket with status 4000 + its HTTP code, e.g. 4404 when the file must be
// opened again.
func (ch *CollabHandler) Collaborate(c *gin.Context) {
	fileID, err := strconv.ParseUint(c.Param("fileId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid file id"})
		return
	}

	ws, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return // Accept has already responded
	}
	ws.SetReadLimit(16 << 20)

	resp := ch.service.Collaborate(collabservice.CollaborateArgs{
		Ctx:    c.Request.Context(),
		FileID: fileID,
		Conn:   conn{ws},
	})
	if resp.Common.Code >= 300 {
		ws.Close(websocket.StatusCode(4000+int(resp.Common.Code)), resp.Common.Message)
		return
	}
	ws.Close(websocket.StatusNormalClosure, "")
}

// conn adapts a WebSocket to collabservice.Conn using binary messages.
type conn struct {
	ws *websocket.Conn
}

func (c conn) Read(ctx context.Context) ([]byte, error) {
	_, msg, err := c.ws.Read(ctx)
	return msg, err
}

func (c conn) Write(ctx context.Context, msg []byte) error {
	return c.ws.Write(ctx, websocket.MessageBinary, msg)
}
