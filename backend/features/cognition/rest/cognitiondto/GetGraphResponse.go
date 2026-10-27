package cognitiondto

import (
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// GetGraphResponse carries the graph as its own JSON document
// (cognition.Graph), which is already the wire format: the format graphs
// are stored, exported and imported in.
type GetGraphResponse struct {
	rest.Status
	Info     *GraphInfo                `json:"info,omitempty"`
	Graph    *cognition.Graph          `json:"graph,omitempty"`
	Pins     map[string]cognition.Pins `json:"pins,omitempty"` // each node's pins, by node ID
	Problems []cognition.Problem       `json:"problems,omitempty"`
}

// GetGraphResponseFrom converts the service's GetGraphResponse.
func GetGraphResponseFrom(resp cognitionservice.GetGraphResponse) GetGraphResponse {
	out := GetGraphResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Graph.ID == "" {
		return out
	}
	info, g := GraphInfoFrom(resp.Info), resp.Graph
	out.Info, out.Graph, out.Problems = &info, &g, resp.Problems
	out.Pins = make(map[string]cognition.Pins, len(resp.Pins))
	for id, p := range resp.Pins {
		out.Pins[id] = pinsOf(p)
	}
	return out
}

// pinsOf is p with empty lists in place of nil ones, so the browser always
// gets arrays.
func pinsOf(p cognition.Pins) cognition.Pins {
	if p.In == nil {
		p.In = []cognition.Pin{}
	}
	if p.Out == nil {
		p.Out = []cognition.Pin{}
	}
	return p
}
