package impl

import (
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// Agents lists the tenant's live agents by name (forge 07j).
func (s *service) Agents(args authzservice.AgentsArgs) authzservice.AgentsResponse {
	res := args.Resource
	if res == "" {
		res = authz.Tenant(args.Principal)
	}
	if !inTenant(args.Principal, res) {
		return authzservice.AgentsResponse{Common: *outside(args.ServiceArgs, res)}
	}
	if f := check(args.ServiceArgs, "grant.read", res); f != nil {
		return authzservice.AgentsResponse{Common: *f}
	}
	agents, err := authz.ListAgents(args.Context(), args.Principal.TenantID)
	if err != nil {
		return authzservice.AgentsResponse{Common: *internal(args.ServiceArgs, err)}
	}
	slices.SortFunc(agents, func(a, b authz.AgentInfo) int { return strings.Compare(a.Name, b.Name) })
	if agents == nil {
		agents = []authz.AgentInfo{}
	}
	return authzservice.AgentsResponse{Common: features.OK(), Agents: agents}
}
