package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type StreamsResponse struct {
	rest.Status
	QueryError *QueryError `json:"queryError,omitempty"`
	Values     []ValueHits `json:"values"`
}

func StreamsResponseFrom(resp logsservice.StreamsResponse) StreamsResponse {
	return StreamsResponse{Status: rest.StatusFrom(resp.Common), QueryError: QueryErrorFrom(resp.QueryError), Values: ValueHitsFrom(resp.Values)}
}
