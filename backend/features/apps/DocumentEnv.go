package apps

import (
	"slices"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// DocumentEnvOf is the registry's primitives, which documents (exported,
// or made of a draft for forge.check) are checked against.
func DocumentEnvOf(reg *extensions.Registry) DocumentEnv {
	return DocumentEnv{
		Catalog: ui.Catalog(reg),
		Node: func(typ string) bool {
			return slices.ContainsFunc(cognition.Nodes.All(reg), func(n cognition.NodeType) bool { return n.ID == typ })
		},
		Builtin: func(id string) (cognition.Graph, bool) {
			all := cognition.Graphs.All(reg)
			if i := slices.IndexFunc(all, func(b cognition.BuiltIn) bool { return b.ID == id }); i >= 0 {
				return all[i].Graph, true
			}
			return cognition.Graph{}, false
		},
		Query: func(name string) bool {
			return slices.ContainsFunc(live.Queries.All(reg), func(q live.Query) bool { return q.ID == name })
		},
		Contract: BuiltinContracts(reg),
	}
}
