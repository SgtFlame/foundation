package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
)

// GetGraph returns a graph with each node's pins and its problems.
func (s *service) GetGraph(args cognitionservice.GetGraphArgs) cognitionservice.GetGraphResponse {
	if failed := check(args.ServiceArgs, "cognition.read", "graph:"+args.ID); failed != nil {
		return cognitionservice.GetGraphResponse{Common: *failed}
	}
	g, ok := s.engine.Graph(args.ID)
	if !ok {
		return cognitionservice.GetGraphResponse{Common: *args.Fail(http.StatusNotFound, log.Code("graph_not_found").With("graphId", args.ID).New("there is no such graph"))}
	}
	res := s.engine.Resolve(g)
	return cognitionservice.GetGraphResponse{Common: features.OK(), Info: info(g), Graph: g, Pins: res.Pins, Problems: res.Problems}
}
