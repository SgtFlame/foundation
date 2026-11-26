package cognition

import (
	"context"
	"sync"
)

// Scope holds values shared by a run and every graph it calls or starts
// (forge 15b: the passages an agent's run numbers, one numbering across
// all its model calls). The engine puts one on a run's context when the
// context has none, so called graphs inherit their caller's.
type Scope struct {
	mu     sync.Mutex
	values map[any]any
}

type scopeKey struct{}

// WithScope is ctx with a new Scope, unless it already carries one.
func WithScope(ctx context.Context) context.Context {
	if ScopeOf(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, scopeKey{}, &Scope{values: map[any]any{}})
}

// ScopeOf is the scope of the run ctx belongs to, nil outside a run.
func ScopeOf(ctx context.Context) *Scope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(scopeKey{}).(*Scope)
	return s
}

// Value is the scope's value for key, made by make on first use. A nil
// scope makes a new value each time.
func (s *Scope) Value(key any, make func() any) any {
	if s == nil {
		return make()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	if !ok {
		v = make()
		s.values[key] = v
	}
	return v
}
