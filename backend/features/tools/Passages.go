package tools

import (
	"context"
	"slices"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// Passages are the passages an agent's run has numbered so far (forge
// 15a), kept on its context so the tools it calls can see what a text they
// are given cites: Mail's mail.send refuses a draft citing someone's
// private memory. Only the Agent node makes them; a chat round keeps its
// own as RoundSegments.
type Passages struct {
	mu   sync.Mutex
	list []retrieval.Scored
}

type passagesKey struct{}

// WithPassages is ctx carrying a new, empty Passages.
func WithPassages(ctx context.Context) (context.Context, *Passages) {
	p := &Passages{}
	return context.WithValue(ctx, passagesKey{}, p), p
}

// PassagesFrom are the passages of the agent run ctx belongs to, nil
// outside one.
func PassagesFrom(ctx context.Context) *Passages {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(passagesKey{}).(*Passages)
	return p
}

// Add keeps numbered passages.
func (p *Passages) Add(segments []retrieval.Scored) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, segments...)
}

// All are the passages kept, in the order they were numbered.
func (p *Passages) All() []retrieval.Scored {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.list)
}
