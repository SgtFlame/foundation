package retrieval

import "context"

// Scope narrows every retrieval a call makes, whatever options it runs
// with (forge 16c): a chat round sets the Volumes its workspace leaves
// unchecked, and kb.search and RAG Search's gather both leave them out.
// Options a model node sets later (an Agent node's Retrieval group, Run
// mode) replace the round's options but never this scope, so nothing a
// round runs inline searches a Volume its workspace left out. Agent runs
// on triggers and jobs carry no scope.
type Scope struct {
	// ExceptVolumeIDs are never searched.
	ExceptVolumeIDs []uint64
}

type scopeKey struct{}

// WithScope is ctx whose retrievals are narrowed by s.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// ScopeFrom is the scope ctx carries; the zero Scope narrows nothing.
func ScopeFrom(ctx context.Context) Scope {
	if ctx == nil {
		return Scope{}
	}
	s, _ := ctx.Value(scopeKey{}).(Scope)
	return s
}
