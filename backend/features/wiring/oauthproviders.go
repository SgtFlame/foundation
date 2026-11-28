package wiring

import (
	"github.com/SgtFlame/foundation/backend/features/connectors/oauth/providers"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// OAuth providers (forge 26a-2): Google, Microsoft and Slack, before the
// connectors.oauth section, which serve registers after ContributeAll.
func init() {
	Register(Feature{
		Name: "oauthproviders",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			providers.Contribute(reg)
			return nil
		},
	})
}
