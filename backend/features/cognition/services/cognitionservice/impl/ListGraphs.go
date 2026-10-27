package impl

import (
	"cmp"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
)

// ListGraphs lists the graphs, by kind and then name.
func (s *service) ListGraphs(args cognitionservice.ListGraphsArgs) cognitionservice.ListGraphsResponse {
	if failed := check(args.ServiceArgs, "cognition.read", "graphs"); failed != nil {
		return cognitionservice.ListGraphsResponse{Common: *failed}
	}
	var out []cognitionservice.GraphInfo
	for _, g := range s.engine.Graphs() {
		out = append(out, info(g))
	}
	slices.SortFunc(out, func(a, b cognitionservice.GraphInfo) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	return cognitionservice.ListGraphsResponse{Common: features.OK(), Graphs: out}
}
