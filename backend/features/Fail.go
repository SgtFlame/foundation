package features

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/samber/oops"
)

// OK is the response of an operation that succeeded.
func OK() ServiceResponse {
	return ServiceResponse{Code: http.StatusOK}
}

// Fail turns err into the response of an operation that failed with status,
// and logs it once: 5xx at error, 4xx at debug. Callers and handlers never
// log the same failure again.
//
// Message is err's oops Public text when it has one. Otherwise a 4xx shows
// err's own text, so 4xx errors must name nothing private (folders relative
// to the root, never server paths), and a 5xx shows only its status text.
func (a ServiceArgs) Fail(status uint16, err error) *ServiceResponse {
	ctx := a.Context()
	resp := &ServiceResponse{Code: status, Trace: logging.TraceFrom(ctx)}
	if o, ok := oops.AsOops(err); ok {
		if code := o.Code(); code != nil {
			resp.ErrorCode = fmt.Sprint(code)
		}
		resp.Message = o.Public()
	}
	if resp.Message == "" {
		if status < http.StatusInternalServerError {
			resp.Message = err.Error()
		} else {
			resp.Message = http.StatusText(int(status))
		}
	}

	level := slog.LevelDebug
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	logging.With("status", status).Log(ctx, level, err, "%s", err)
	return resp
}
