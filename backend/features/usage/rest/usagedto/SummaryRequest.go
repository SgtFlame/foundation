package usagedto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

type SummaryRequest struct {
	From            time.Time `json:"from"` // RFC 3339; omitted leaves the range open
	To              time.Time `json:"to"`
	Bucket          string    `json:"bucket,omitempty"`  // hour, day or month
	GroupBy         string    `json:"groupBy,omitempty"` // feature, model, level or conversation
	Feature         string    `json:"feature,omitempty"`
	TZOffsetMinutes int       `json:"tzOffsetMinutes,omitempty"` // minutes east of UTC
	Limit           int       `json:"limit,omitempty"`
}

// ToArgs converts the request to the service's SummaryArgs.
func (r SummaryRequest) ToArgs(args features.ServiceArgs) usageservice.SummaryArgs {
	return usageservice.SummaryArgs{
		ServiceArgs: args, From: r.From, To: r.To, Bucket: r.Bucket, GroupBy: r.GroupBy, Feature: r.Feature,
		TZOffsetMinutes: r.TZOffsetMinutes, Limit: r.Limit,
	}
}
