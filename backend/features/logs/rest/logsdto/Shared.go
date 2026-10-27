package logsdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// Range is a query's time range, RFC 3339; omitted, end is now and start an
// hour before it.
type Range struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (r Range) toRange() logsservice.Range {
	return logsservice.Range{Start: r.Start, End: r.End}
}

// QueryError is where a LogsQL query stopped parsing.
type QueryError struct {
	Message string `json:"message"`
	Offset  int    `json:"offset"` // in bytes; -1 when unknown
}

func QueryErrorFrom(qe *logsservice.QueryError) *QueryError {
	if qe == nil {
		return nil
	}
	return &QueryError{Message: qe.Message, Offset: qe.Offset}
}

type ValueHits struct {
	Value string `json:"value"`
	Hits  int64  `json:"hits"`
}

func ValueHitsFrom(values []logsservice.ValueHits) []ValueHits {
	out := make([]ValueHits, 0, len(values))
	for _, v := range values {
		out = append(out, ValueHits{Value: v.Value, Hits: v.Hits})
	}
	return out
}
