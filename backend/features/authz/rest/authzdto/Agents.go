package authzdto

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// Agent is an agent principal (forge 07j).
type Agent = authz.AgentInfo

type AgentsResponse struct {
	rest.Status
	Agents []Agent `json:"agents"`
}

func AgentsResponseFrom(resp authzservice.AgentsResponse) AgentsResponse {
	return AgentsResponse{Status: rest.StatusFrom(resp.Common), Agents: resp.Agents}
}
