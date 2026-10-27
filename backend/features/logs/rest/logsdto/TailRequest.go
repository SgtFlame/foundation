package logsdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type TailRequest struct {
	Query string `json:"query"`
}

// ToArgs converts the request to the service's TailArgs.
func (r TailRequest) ToArgs(args features.ServiceArgs) logsservice.TailArgs {
	return logsservice.TailArgs{ServiceArgs: args, Query: r.Query}
}

// TailFailed is the body of a tail that could not start.
type TailFailed struct {
	rest.Status
	QueryError *QueryError `json:"queryError,omitempty"`
}
