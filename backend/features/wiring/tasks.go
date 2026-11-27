package wiring

import (
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trackerfake"
)

// Tasks (forge 30g): the tasks.tracker@1 contract and the Tasks test server
// with its control call.
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
	})
}
