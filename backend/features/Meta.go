package features

import "context"

// Meta says what a piece of work is for, so the usage it causes can be
// attributed (documentation/plans/Usage.md). Callers set what they know and
// leave the rest empty.
type Meta struct {
	Feature        string // e.g. "chat", "embed-index", "embed-query"
	Level          string // the chat level, empty outside the chat levels
	ConversationID uint64
	RoundID        uint64
	FileID         uint64
	RunID          uint64 // the Cognition run, and NodeID the node in it, that made the call
	NodeID         string
}

// WithFeature is m with Feature set, unless it already has one.
func (m Meta) WithFeature(feature string) Meta {
	if m.Feature == "" {
		m.Feature = feature
	}
	return m
}

type callKey struct{}

type call struct {
	principal Principal
	meta      Meta
}

// CallFrom is the principal and Meta that ServiceArgs.CallContext stored in
// ctx; ok is false when there are none.
func CallFrom(ctx context.Context) (p Principal, m Meta, ok bool) {
	c, ok := ctx.Value(callKey{}).(call)
	return c.principal, c.meta, ok
}
