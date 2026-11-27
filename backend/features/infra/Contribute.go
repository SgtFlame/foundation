package infra

import (
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Contribute registers the configurations' projectors, ops and queries.
// Later parts add the diff, the contract, plan and apply here.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	contributeConfig(reg)
	contributeDiff(reg)
	contributeExport(reg)
	contributeCompute(reg)
}

// Deps are what infra needs at run time, made after the registry.
type Deps struct{}
