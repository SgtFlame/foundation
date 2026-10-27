// Package providers holds what every AI provider (Gemini, OpenAI, Azure AI, ...)
// shares; each provider lives in its own subpackage.
package providers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Error is a provider failure carrying the HTTP status the services report.
type Error struct {
	Code    uint16
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func Errorf(code uint16, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrNotConfigured is returned by a provider without credentials.
var ErrNotConfigured = &Error{Code: http.StatusServiceUnavailable, Message: "AI provider is not configured"}

// Code is err's status: its own for an *Error, 502 for any other upstream failure.
func Code(err error) uint16 {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return http.StatusBadGateway
}

// Log is the ai feature's logging and error builder.
var Log = logging.In("ai")

// Wrap makes a provider failure an oops error that shows users the
// provider's own message; Code(err) still gives its status.
func Wrap(err error) error {
	code := "provider_failed"
	if errors.Is(err, ErrNotConfigured) {
		code = "provider_not_configured"
	}
	return Log.Code(code).Public(err.Error()).Wrap(err)
}
