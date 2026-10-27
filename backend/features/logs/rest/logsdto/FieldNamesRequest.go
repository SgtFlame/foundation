package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

type FieldNamesRequest struct {
	Range
	Query string `json:"query"`
}

// ToArgs converts the request to the service's FieldNamesArgs.
func (r FieldNamesRequest) ToArgs(args features.ServiceArgs) logsservice.FieldNamesArgs {
	return logsservice.FieldNamesArgs{ServiceArgs: args, Range: r.toRange(), Query: r.Query}
}
