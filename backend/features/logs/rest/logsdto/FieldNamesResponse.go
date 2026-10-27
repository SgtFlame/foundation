package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type FieldNamesResponse struct {
	rest.Status
	QueryError *QueryError `json:"queryError,omitempty"`
	Names      []ValueHits `json:"names"`
}

func FieldNamesResponseFrom(resp logsservice.FieldNamesResponse) FieldNamesResponse {
	return FieldNamesResponse{Status: rest.StatusFrom(resp.Common), QueryError: QueryErrorFrom(resp.QueryError), Names: ValueHitsFrom(resp.Names)}
}
