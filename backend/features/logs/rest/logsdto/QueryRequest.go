package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

type QueryRequest struct {
	Range
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// ToArgs converts the request to the service's QueryArgs.
func (r QueryRequest) ToArgs(args features.ServiceArgs) logsservice.QueryArgs {
	return logsservice.QueryArgs{ServiceArgs: args, Range: r.toRange(), Query: r.Query, Limit: r.Limit}
}
