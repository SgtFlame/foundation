package retrievaldto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
)

type RetrieveResponse struct {
	rest.Status
	Segments      []Segment `json:"segments,omitempty"`
	Candidates    int       `json:"candidates"`
	BudgetTokens  int       `json:"budgetTokens"`
	VectorSkipped bool      `json:"vectorSkipped,omitempty"`
}

// RetrieveResponseFrom converts the service's RetrieveResponse; its code is
// carried by the HTTP status rather than the body.
func RetrieveResponseFrom(resp retrievalservice.RetrieveResponse) RetrieveResponse {
	out := RetrieveResponse{Status: rest.StatusFrom(resp.Common), Candidates: resp.Candidates, BudgetTokens: resp.BudgetTokens, VectorSkipped: resp.VectorSkipped}
	for _, s := range resp.Segments {
		out.Segments = append(out.Segments, SegmentFrom(s))
	}
	return out
}
