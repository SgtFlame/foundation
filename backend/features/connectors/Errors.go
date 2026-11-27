package connectors

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Error codes a connector call returns.
const (
	CodeAuth         = "auth"
	CodeNotFound     = "notFound"
	CodeConflict     = "conflict"
	CodeInvalid      = "invalid"
	CodeRateLimited  = "rateLimited"
	CodeUnavailable  = "unavailable"
	CodeNotSupported = "notSupported"
)

// Codes are every error code, in the order they are documented.
var Codes = []string{CodeAuth, CodeNotFound, CodeConflict, CodeInvalid, CodeRateLimited, CodeUnavailable, CodeNotSupported}

// Error is a typed connector error, as a reply carries it. Its
// Retryable and RetryAfter methods are what the job runner reads
// (jobs.Classify), so a job calling a connector retries a busy service.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Retry   bool   `json:"retryable"`
	// RetryAfterMs is how long to wait before retrying, when the service
	// says (rateLimited's Retry-After).
	RetryAfterMs int64 `json:"retryAfter,omitempty"`
	// Detail narrows the code: DetailLocked.
	Detail string `json:"detail,omitempty"`
}

// DetailLocked is an auth error from a connection whose key waits to be
// unlocked (git.ssh, forge 24f): nothing is wrong with the connection, so
// it is not marked failed.
const DetailLocked = "locked"

// Locked makes the error of a call on a locked connection.
func Locked(message string) *Error {
	return &Error{Code: CodeAuth, Message: message, Detail: DetailLocked}
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Retryable reports whether trying again later may succeed.
func (e *Error) Retryable() bool { return e.Retry }

// RetryAfter is how long to wait first; 0 leaves it to the backoff.
func (e *Error) RetryAfter() time.Duration { return time.Duration(e.RetryAfterMs) * time.Millisecond }

// Errorf makes a typed error; unavailable and rateLimited are retryable.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Retry: code == CodeUnavailable || code == CodeRateLimited}
}

// AsError finds a typed error in err's chain; any other error is
// unavailable and not retryable (a bug, not a busy service).
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeUnavailable, Message: err.Error()}
}

// ValidCode reports whether code is an error code.
func ValidCode(code string) bool { return slices.Contains(Codes, code) }

// NotRetryable makes e one that trying again does not fix.
func (e *Error) NotRetryable() *Error {
	e.Retry = false
	return e
}
