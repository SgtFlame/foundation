package wiring

import (
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/notifications"
)

// Notifications (forge 26e-1): the notification resource type, the tenant's
// notification entity type, Notify and the notification queries.
func init() {
	Register(Feature{
		Name: "notifications",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			notifications.ContributeAccess(reg)
			notifications.Contribute(reg, func() notifications.Deps { return env.RecordDeps() })
			return nil
		},
	})
}
