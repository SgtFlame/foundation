package appdto

import (
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type CheckResponse struct {
	rest.Status
	Problems []live.Problem `json:"problems"`
}

// CheckResponseFrom converts the service's CheckResponse.
func CheckResponseFrom(resp appservice.CheckResponse) CheckResponse {
	return CheckResponse{Status: rest.StatusFrom(resp.Common), Problems: problems(resp.Problems)}
}
