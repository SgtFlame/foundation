package appdto

import (
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ImportResponse struct {
	rest.Status
	Plan     appservice.Plan `json:"plan"`
	Imported bool            `json:"imported"`
	Version  int             `json:"version,omitempty"`
	Problems []live.Problem  `json:"problems"`
}

// ImportResponseFrom converts the service's ImportResponse.
func ImportResponseFrom(resp appservice.ImportResponse) ImportResponse {
	plan := resp.Plan
	if plan.Parts == nil {
		plan.Parts = []appservice.PartPlan{}
	}
	return ImportResponse{Status: rest.StatusFrom(resp.Common), Plan: plan, Imported: resp.Imported, Version: resp.Version, Problems: problems(resp.Problems)}
}
