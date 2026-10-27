package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
)

// NodeTypes returns the node types and the data types.
func (s *service) NodeTypes(args cognitionservice.NodeTypesArgs) cognitionservice.NodeTypesResponse {
	if failed := check(args.ServiceArgs, "cognition.read", "node-types"); failed != nil {
		return cognitionservice.NodeTypesResponse{Common: *failed}
	}
	return cognitionservice.NodeTypesResponse{Common: features.OK(), NodeTypes: s.engine.NodeTypes(), Types: cognition.Types.All(s.engine.Registry())}
}
