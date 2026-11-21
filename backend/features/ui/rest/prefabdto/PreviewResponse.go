package prefabdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

type PreviewResponse struct {
	rest.Status
	Tree     *ui.Node     `json:"tree,omitempty"`
	Problems []ui.Problem `json:"problems"`
}

// PreviewResponseFrom converts the service's PreviewResponse.
func PreviewResponseFrom(resp prefabservice.PreviewResponse) PreviewResponse {
	problems := resp.Problems
	if problems == nil {
		problems = []ui.Problem{}
	}
	return PreviewResponse{Status: rest.StatusFrom(resp.Common), Tree: resp.Tree, Problems: problems}
}
