package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/samber/oops"
)

type contextKey struct{}

// WithContext stores b in ctx for every record logged and error made with
// it: the request middleware stores the trace, user and tenant this way. It
// also stores the equivalent oops builder, so oops.FromContext(ctx) agrees.
func WithContext(ctx context.Context, b Builder) context.Context {
	ctx = context.WithValue(ctx, contextKey{}, b)
	return oops.WithBuilder(ctx, b.oops())
}

// FromContext is the builder stored in ctx, or an empty one. Use it to make
// errors carrying the request's context; logging methods read ctx themselves.
func FromContext(ctx context.Context) Builder {
	if ctx == nil {
		return Builder{}
	}
	b, _ := ctx.Value(contextKey{}).(Builder)
	return b
}

// TraceFrom is the trace ID of the request ctx belongs to, if any.
func TraceFrom(ctx context.Context) string {
	return FromContext(ctx).trace
}

// NewTraceID returns 32 random hex digits, the same form as the browser's.
func NewTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
