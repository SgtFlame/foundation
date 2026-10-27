package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type IngestResponse struct {
	rest.Status
	Accepted int `json:"accepted"`
}

func IngestResponseFrom(resp logsservice.IngestResponse) IngestResponse {
	return IngestResponse{Status: rest.StatusFrom(resp.Common), Accepted: resp.Accepted}
}
