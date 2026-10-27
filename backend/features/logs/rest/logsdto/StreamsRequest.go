package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

type StreamsRequest struct {
	Range
	Query string `json:"query"`
	Field string `json:"field,omitempty"` // lists this stream field's values instead of the streams
}

// ToArgs converts the request to the service's StreamsArgs.
func (r StreamsRequest) ToArgs(args features.ServiceArgs) logsservice.StreamsArgs {
	return logsservice.StreamsArgs{ServiceArgs: args, Range: r.toRange(), Query: r.Query, Field: r.Field}
}
