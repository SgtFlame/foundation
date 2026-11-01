package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
)

// NodeTypes returns the node types, the data types, the graph kinds and the
// conversions edges may make.
func (s *service) NodeTypes(args cognitionservice.NodeTypesArgs) cognitionservice.NodeTypesResponse {
	if failed := check(args.ServiceArgs, "cognition.read", "node-types"); failed != nil {
		return cognitionservice.NodeTypesResponse{Common: *failed}
	}
	cat := s.engine.Catalog()
	return cognitionservice.NodeTypesResponse{Common: features.OK(), NodeTypes: cat.Types, Types: cognition.Types.All(s.engine.Registry()), Kinds: cat.Kinds, Conversions: cat.Conversions}
}
