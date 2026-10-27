package cognitiondto

import (
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListGraphsResponse struct {
	rest.Status
	Graphs []GraphInfo `json:"graphs"`
}

// ListGraphsResponseFrom converts the service's ListGraphsResponse; its code
// is carried by the HTTP status rather than the body.
func ListGraphsResponseFrom(resp cognitionservice.ListGraphsResponse) ListGraphsResponse {
	out := ListGraphsResponse{Status: rest.StatusFrom(resp.Common), Graphs: []GraphInfo{}}
	for _, g := range resp.Graphs {
		out.Graphs = append(out.Graphs, GraphInfoFrom(g))
	}
	return out
}
