package engine

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features/cognition"
)

// Sandbox is how a sandboxed run treats External nodes (sandbox.run, forge
// 07j), carried on the context so the runs its tools start (an app tool's
// function graph) are sandboxed too.
type Sandbox struct {
	Stub bool
	Real func(t cognition.NodeType) bool
}

type sandboxKey struct{}

// WithSandbox carries s to the runs started under ctx.
func WithSandbox(ctx context.Context, s Sandbox) context.Context {
	return context.WithValue(ctx, sandboxKey{}, s)
}

// SandboxOf is the sandbox ctx carries.
func SandboxOf(ctx context.Context) (Sandbox, bool) {
	s, ok := ctx.Value(sandboxKey{}).(Sandbox)
	return s, ok
}

// RealModels runs model calls for real and stubs every other External node
// (connector calls, the object store, grants).
func RealModels(t cognition.NodeType) bool { return t.Model }
