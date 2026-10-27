package usageservice

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/usage"
)

// Service reads the usage records of the principal's tenant: summed for the
// Dashboard, or one by one (documentation/Usage.md).
type Service interface {
	Summary(args SummaryArgs) SummaryResponse
	Records(args RecordsArgs) RecordsResponse
	Capture(args CaptureArgs) CaptureResponse
}

// RecordsArgs pages through the records newest first. Zero times leave the
// range open; Feature filters when set.
type RecordsArgs struct {
	features.ServiceArgs
	From, To time.Time
	Feature  string
	Page     int // from 0
	PageSize int // 50 when 0, at most 500
}

type RecordsResponse struct {
	Common  features.ServiceResponse
	Records []usage.Record
	Total   int64 // matching records across all pages
}

// CaptureArgs names the record whose captured text to read.
type CaptureArgs struct {
	features.ServiceArgs
	RecordID uint64
}

// CaptureResponse is 404 capture_not_found when the call was not captured
// or its capture expired.
type CaptureResponse struct {
	Common  features.ServiceResponse
	Capture usage.Capture
}

// SummaryArgs sums the records in [From, To), optionally per time bucket
// and per group. Zero times leave the range open; Feature filters when set.
type SummaryArgs struct {
	features.ServiceArgs
	From, To time.Time
	Bucket   string // "", hour, day or month
	GroupBy  string // "", feature, model, level or conversation
	Feature  string
	// TZOffsetMinutes shifts the buckets to the viewer's time zone: the
	// minutes east of UTC (UTC+2 is 120).
	TZOffsetMinutes int
	// Limit keeps the groups with the highest cost; 0 keeps them all. With a
	// bucket the limit applies to each group's total across buckets.
	Limit int
}

// SummaryResponse holds the totals of the range and a row per bucket and
// group, in bucket order, the costliest group first. Costs are summed
// whatever their currency; every model in the default catalog is priced
// in USD (Usage.md question 1).
type SummaryResponse struct {
	Common features.ServiceResponse
	Totals Totals
	Rows   []SummaryRow
}

type Totals struct {
	Requests       int64
	InputTokens    int64 // includes CachedTokens
	CachedTokens   int64
	OutputTokens   int64
	ThinkingTokens int64
	EmbedTokens    int64
	CostMicros     int64
}

type SummaryRow struct {
	Bucket string // e.g. "2026-10-23" for a day, "2026-10-23 14" for an hour, "2026-10" for a month; "" without a bucket
	Group  string // the feature, model, level or conversation ID; "" without a group
	Totals
}
