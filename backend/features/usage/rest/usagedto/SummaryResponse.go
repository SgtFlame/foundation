package usagedto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

type SummaryResponse struct {
	rest.Status
	Totals Totals       `json:"totals"`
	Rows   []SummaryRow `json:"rows,omitempty"`
}

type Totals struct {
	Requests       int64 `json:"requests"`
	InputTokens    int64 `json:"inputTokens"` // includes cachedTokens
	CachedTokens   int64 `json:"cachedTokens"`
	OutputTokens   int64 `json:"outputTokens"`
	ThinkingTokens int64 `json:"thinkingTokens"`
	EmbedTokens    int64 `json:"embedTokens"`
	CostMicros     int64 `json:"costMicros"`
}

type SummaryRow struct {
	Bucket string `json:"bucket,omitempty"`
	Group  string `json:"group,omitempty"`
	Totals
}

// SummaryResponseFrom converts the service's SummaryResponse; its code is
// carried by the HTTP status rather than the body.
func SummaryResponseFrom(resp usageservice.SummaryResponse) SummaryResponse {
	out := SummaryResponse{Status: rest.StatusFrom(resp.Common), Totals: Totals(resp.Totals)}
	for _, r := range resp.Rows {
		out.Rows = append(out.Rows, SummaryRow{Bucket: r.Bucket, Group: r.Group, Totals: Totals(r.Totals)})
	}
	return out
}
