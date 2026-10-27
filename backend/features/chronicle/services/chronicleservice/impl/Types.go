package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// Types returns the entry types.
func (s *service) Types(args chronicleservice.TypesArgs) chronicleservice.TypesResponse {
	if failed := check(args.ServiceArgs, "chronicle.read", 0); failed != nil {
		return chronicleservice.TypesResponse{Common: *failed}
	}
	return chronicleservice.TypesResponse{Common: features.OK(), Types: chronicle.Types}
}
