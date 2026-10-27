package logsdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

type HitsRequest struct {
	Range
	Query  string `json:"query"`
	StepMs int64  `json:"stepMs,omitempty"` // a 60th of the range when omitted
	Field  string `json:"field,omitempty"`  // splits the counts by this field's values
}

// ToArgs converts the request to the service's HitsArgs.
func (r HitsRequest) ToArgs(args features.ServiceArgs) logsservice.HitsArgs {
	return logsservice.HitsArgs{ServiceArgs: args, Range: r.toRange(), Query: r.Query, Step: time.Duration(r.StepMs) * time.Millisecond, Field: r.Field}
}
