package wiring

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/docs"
	docsnodes "github.com/SgtFlame/foundation/backend/features/docs/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
)

// Docs (forge 35a-2): the nodes the docs library app's graphs reach a
// Volume with, and page.saved whenever a page's document file is written.
func init() {
	Register(Feature{
		Name: "docs",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			docsnodes.Contribute(reg, func() docs.Deps {
				kb := env.Knowledge()
				return docs.Deps{Root: kb.Root, Volumes: kb.Volumes}
			})
			return nil
		},
		Start: func(ctx context.Context, env *Env) error {
			stop := docs.FollowSaves(ctx, env.Knowledge().Root, env.VolumeFiles(), func() *bus.Bus { return busOf(env.Connections()) })
			go func() { <-ctx.Done(); stop() }()
			return nil
		},
	})
}

func busOf(d connections.Deps) *bus.Bus { return d.Bus }
