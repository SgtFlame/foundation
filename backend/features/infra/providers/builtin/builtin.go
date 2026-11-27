// Package builtin gathers the infra.compute@1 provider types built into
// Foundation (forge 22g, 22l to 22n), for serve.go and Forge's catalog.
package builtin

import (
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/lxd"
)

// Types are the provider types.
func Types() []connectors.Type {
	return []connectors.Type{lxd.Type()}
}

// Impls are their adapter implementations.
func Impls() []adapter.Impl {
	return []adapter.Impl{lxd.Impl(lxd.Options{})}
}

// Contribute registers the provider types (infra.compute@1 itself is
// infra's).
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, "infra", nil, Types())
}
