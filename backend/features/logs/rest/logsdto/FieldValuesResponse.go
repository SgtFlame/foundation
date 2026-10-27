package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type FieldValuesResponse struct {
	rest.Status
	QueryError *QueryError `json:"queryError,omitempty"`
	Values     []ValueHits `json:"values"`
}

func FieldValuesResponseFrom(resp logsservice.FieldValuesResponse) FieldValuesResponse {
	return FieldValuesResponse{Status: rest.StatusFrom(resp.Common), QueryError: QueryErrorFrom(resp.QueryError), Values: ValueHitsFrom(resp.Values)}
}
