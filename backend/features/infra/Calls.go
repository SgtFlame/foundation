package infra

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// RegisterToolAndCall registers a server tool and a live call of the same
// id running it, ending with one result event {content, summary}: the
// tool for the chat and graphs, the call for the forge tools and pages
// that call it over the live connection.
func RegisterToolAndCall(reg *extensions.Registry, t tools.Tool) {
	tools.Tools.Register(reg, t)
	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: t.ID, Source: t.Source}, Title: t.Title,
		Description: t.Description + " Ends with one result event {content, summary}.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			args := c.Args
			if len(args) == 0 {
				args = []byte(`{}`)
			}
			if err := tools.CheckArgs(t.Input, args); err != nil {
				return c.Fail(http.StatusBadRequest, log.Code("invalid_args").Wrap(err))
			}
			res, err := t.Run(tools.Call{ServiceArgs: c.ServiceArgs, ID: t.ID, Name: t.ID, Args: args})
			if err != nil {
				return c.Fail(http.StatusBadRequest, log.Wrap(err))
			}
			c.Emit("result", map[string]any{"content": res.Content, "summary": res.Summary})
			return nil
		},
	})
}
