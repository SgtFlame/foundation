package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

type FieldValuesRequest struct {
	Range
	Query string `json:"query"`
	Field string `json:"field"`
	Limit int    `json:"limit,omitempty"`
}

// ToArgs converts the request to the service's FieldValuesArgs.
func (r FieldValuesRequest) ToArgs(args features.ServiceArgs) logsservice.FieldValuesArgs {
	return logsservice.FieldValuesArgs{ServiceArgs: args, Range: r.toRange(), Query: r.Query, Field: r.Field, Limit: r.Limit}
}
