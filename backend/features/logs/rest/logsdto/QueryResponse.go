package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type QueryResponse struct {
	rest.Status
	QueryError *QueryError         `json:"queryError,omitempty"`
	Rows       []map[string]string `json:"rows"`
	Limited    bool                `json:"limited,omitempty"`
}

func QueryResponseFrom(resp logsservice.QueryResponse) QueryResponse {
	out := QueryResponse{Status: rest.StatusFrom(resp.Common), QueryError: QueryErrorFrom(resp.QueryError), Limited: resp.Limited, Rows: []map[string]string{}}
	for _, row := range resp.Rows {
		out.Rows = append(out.Rows, row)
	}
	return out
}
