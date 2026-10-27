package confighandlers

import (
	"io"
	"time"

	"github.com/SgtFlame/foundation/backend/features/config/rest/configdto"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// keepAlive is how often an idle stream sends a comment, so proxies keep it open.
const keepAlive = 30 * time.Second

// Events handles GET /api/v1/config/events: a Server-Sent Events stream with
// a "changed" event for every stored change, until the client goes away.
func (ch *ConfigHandler) Events(c *gin.Context) {
	watch := ch.service.Watch(configservice.WatchArgs{ServiceArgs: rest.Args(c)})
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.SSEvent("ready", "")
	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()
	c.Stream(func(w io.Writer) bool {
		select {
		case e, ok := <-watch.Events:
			if !ok {
				return false
			}
			c.SSEvent("changed", configdto.ChangedEvent{Section: e.Section, Scope: e.Scope})
		case <-ticker.C:
			_, _ = io.WriteString(w, ": keep-alive\n\n")
		}
		return true
	})
}
