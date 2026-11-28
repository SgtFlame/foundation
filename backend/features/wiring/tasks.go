package wiring

import (
	"context"

	foundation "github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/tasks"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trackerfake"
)

// Tasks (forge 30g): the tasks.tracker@1 contract and the Tasks test server
// with its control call; the Tasks library app seeded in every tenant
// (forge 30c).
func init() {
	Register(Feature{
		Name: "tasks",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			tracker.Contribute(reg)
			trackerfake.Contribute(reg, func() trackerfake.Dispatcher {
				if d := env.Connectors().Dispatcher; d != nil {
					return d
				}
				return nil
			})
			return nil
		},
		Adapter: func(o *adapter.Options) {
			o.Types = append(o.Types, trackerfake.Type())
			o.Contracts = append(o.Contracts, tracker.Contract(), trackerfake.ControlContract())
			o.Impls = append(o.Impls, trackerfake.Default.Impl())
		},
		Start: func(ctx context.Context, s Started) error {
			ids, err := s.Tenants()
			if err != nil {
				return err
			}
			for _, t := range ids {
				if t == foundation.GlobalTenantID {
					continue
				}
				// one tenant's failure leaves the others and the server running
				if err := tasks.SeedApp(foundation.BackgroundArgs(ctx), t, s.Store, s.Apps, s.Bus); err != nil {
					logging.In("tasks").Error(ctx, err, "cannot seed the Tasks app in tenant %d", t)
				}
			}
			return nil
		},
	})
}
