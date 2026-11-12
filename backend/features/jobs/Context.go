package jobs

import (
	"context"
	"errors"
	"time"
)

// Info is what a running job's graph knows of its job (the Job info node).
type Info struct {
	ID      string         `json:"id"`
	Attempt int            `json:"attempt"`
	Queue   string         `json:"queue"`
	App     string         `json:"app,omitempty"`
	Trigger string         `json:"trigger,omitempty"`
	Event   map[string]any `json:"event,omitempty"` // the trigger's event or tick
}

type infoKey struct{}

// WithInfo is ctx for a job's run.
func WithInfo(ctx context.Context, i Info) context.Context { return context.WithValue(ctx, infoKey{}, i) }

// InfoOf is the job a run is for; ok is false outside a job.
func InfoOf(ctx context.Context) (Info, bool) {
	i, ok := ctx.Value(infoKey{}).(Info)
	return i, ok
}

// Failure is how a job's graph fails on purpose (the Fail node): with
// Retryable, the job is tried again, after After when it is set (else the
// backoff's wait), until it runs out of attempts.
type Failure struct {
	Message   string
	Retryable bool
	After     time.Duration
}

func (f *Failure) Error() string { return f.Message }

// retryable is an error that says whether a job failing with it is worth
// retrying, such as a connector's unavailable or rateLimited (forge 07b),
// without importing this package.
type retryable interface{ Retryable() bool }

// retryAfter is an error that says how long to wait before retrying.
type retryAfter interface{ RetryAfter() time.Duration }

// Classify says whether a run's error is worth retrying and after how
// long (0: the backoff's wait). An error is retried only when something in
// its chain says so.
func Classify(err error) (retry bool, after time.Duration) {
	var f *Failure
	if errors.As(err, &f) {
		return f.Retryable, f.After
	}
	var r retryable
	if errors.As(err, &r) && r.Retryable() {
		var a retryAfter
		if errors.As(err, &a) {
			after = a.RetryAfter()
		}
		return true, after
	}
	return false, 0
}

// Backoff is the wait before retrying after attempt (from 1): base doubled
// each attempt, at most max.
func Backoff(attempt int, base, max time.Duration) time.Duration {
	d := base
	for i := 1; i < attempt && d < max; i++ {
		d *= 2
	}
	return min(d, max)
}
