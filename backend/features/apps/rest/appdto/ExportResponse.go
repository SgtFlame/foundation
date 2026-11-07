package appdto

import (
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ExportResponse struct {
	rest.Status
	Document *apps.Document `json:"document,omitempty"`
	Problems []live.Problem `json:"problems"`
}

// ExportResponseFrom converts the service's ExportResponse.
func ExportResponseFrom(resp appservice.ExportResponse) ExportResponse {
	return ExportResponse{Status: rest.StatusFrom(resp.Common), Document: resp.Document, Problems: problems(resp.Problems)}
}

func problems(ps []live.Problem) []live.Problem {
	if ps == nil {
		return []live.Problem{}
	}
	return ps
}
