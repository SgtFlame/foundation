package cognitiondto

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListRunsResponse struct {
	rest.Status
	Runs []Run `json:"runs"` // newest first
	More bool  `json:"more,omitempty"`
}

// ListRunsResponseFrom converts the service's ListRunsResponse.
func ListRunsResponseFrom(resp cognitionservice.ListRunsResponse) ListRunsResponse {
	out := ListRunsResponse{Status: rest.StatusFrom(resp.Common), Runs: []Run{}, More: resp.More}
	for _, r := range resp.Runs {
		out.Runs = append(out.Runs, RunFrom(r))
	}
	return out
}
