package wiring

import (
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/messaging"
	"github.com/SgtFlame/foundation/backend/features/messaging/msgtest"
)

// Messaging (forge 26f-1): chat.messaging@1, the contract for outside chat
// services, and the Messaging test server with its control call.
func init() {
	Register(Feature{
		Name: "messaging",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			messaging.Contribute(reg)
			msgtest.Contribute(reg)
			msgtest.ContributeLive(reg, func() connections.Deps { return env.Connections() }) // msgtest.call, for forge msgtest
			return nil
		},
		Adapter: func(o *adapter.Options) {
			o.Types = append(o.Types, msgtest.Type())
			o.Contracts = append(o.Contracts, messaging.Contract(), msgtest.ControlContract())
			o.Impls = append(o.Impls, msgtest.Default.Impl())
		},
	})
}
