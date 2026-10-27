package logshandlers

import (
	"io"
	"time"

	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// keepAlive is how often an idle tail sends a comment, so proxies keep it open.
const keepAlive = 30 * time.Second

// Tail handles POST /api/v1/logs/tail, answering with Server-Sent Events: a
// "ready" event, then a "row" event per new record until the client goes
// away. A tail that cannot start gets a plain JSON error with its status.
func (lh *LogsHandler) Tail(c *gin.Context) {
	var req logsdto.TailRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	tail := lh.service.Tail(req.ToArgs(args))
	if tail.Common.Code >= 300 {
		c.JSON(int(tail.Common.Code), logsdto.TailFailed{Status: rest.StatusFrom(tail.Common), QueryError: logsdto.QueryErrorFrom(tail.QueryError)})
		return
	}

	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.SSEvent("ready", "")
	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()
	c.Stream(func(w io.Writer) bool {
		select {
		case row, ok := <-tail.Rows:
			if !ok {
				return false
			}
			c.SSEvent("row", row)
		case <-ticker.C:
			_, _ = io.WriteString(w, ": keep-alive\n\n")
		}
		return true
	})
}
