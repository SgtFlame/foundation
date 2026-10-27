package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// Explain decides for the principal, or for another user of their tenant,
// which needs grant.read on the resource (on the tenant, for a resource
// outside it such as a user's own settings).
func (s *service) Explain(args authzservice.ExplainArgs) authzservice.ExplainResponse {
	who := args.Principal
	if args.UserID != 0 && args.UserID != who.UserID {
		guard := args.Resource
		if authz.TenantOf(guard) != args.Principal.TenantID {
			guard = authz.Tenant(args.Principal)
		}
		if f := check(args.ServiceArgs, "grant.read", guard); f != nil {
			return authzservice.ExplainResponse{Common: *f}
		}
		who = features.Principal{Kind: features.PrincipalUser, UserID: args.UserID, TenantID: args.Principal.TenantID}
	}
	d := s.pol.Explain(args.Context(), who, args.Action, args.Resource)
	return authzservice.ExplainResponse{Common: features.OK(), Decision: d}
}
