package logsdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type HitsResponse struct {
	rest.Status
	QueryError *QueryError  `json:"queryError,omitempty"`
	StepMs     int64        `json:"stepMs"`
	Series     []HitsSeries `json:"series"`
}

type HitsSeries struct {
	Fields     map[string]string `json:"fields"`
	Timestamps []time.Time       `json:"timestamps"`
	Values     []int64           `json:"values"`
	Total      int64             `json:"total"`
}

func HitsResponseFrom(resp logsservice.HitsResponse) HitsResponse {
	out := HitsResponse{Status: rest.StatusFrom(resp.Common), QueryError: QueryErrorFrom(resp.QueryError), StepMs: resp.Step.Milliseconds(), Series: []HitsSeries{}}
	for _, s := range resp.Series {
		out.Series = append(out.Series, HitsSeries{Fields: s.Fields, Timestamps: s.Timestamps, Values: s.Values, Total: s.Total})
	}
	return out
}
