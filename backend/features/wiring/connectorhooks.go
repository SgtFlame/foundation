package wiring

import (
	"github.com/SgtFlame/foundation/backend/features/connectors/hooks"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Connector hooks (forge 26b-1): the hooks' settings section (rate limits and
// the public base URL). The endpoint itself is a route serve adds outside
// /api/v1.
func init() {
	Register(Feature{
		Name: "connectorhooks",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			hooks.ContributeSettings(reg)
			return nil
		},
	})
}
