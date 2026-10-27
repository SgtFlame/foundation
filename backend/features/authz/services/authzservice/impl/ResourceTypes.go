package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// ResourceTypes is every declared resource type, for the permission matrix.
func (s *service) ResourceTypes(args authzservice.ResourceTypesArgs) authzservice.ResourceTypesResponse {
	types := authz.ResourceTypes.All(s.pol.Registry())
	for i := range types {
		for j := range types[i].Actions {
			if types[i].Actions[j].On == "" {
				types[i].Actions[j].On = authz.OnTenant
			}
		}
	}
	return authzservice.ResourceTypesResponse{Common: features.OK(), Types: types}
}
