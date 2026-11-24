package tools

import (
	"context"
	"slices"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// Passages are the passages a run has numbered so far (forge 15a, 15b),
// kept so the tools it calls can see what a text they are given cites
// (Mail's mail.send refuses a draft citing someone's private memory) and
// so a host can keep them as the run's segments. One Passages numbers
// every model call of a run (the Agent nodes and Run mode), as chat's
// citer numbers a round's: a passage found twice keeps its first number.
// A chat round keeps its own as RoundSegments.
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

// PassagesFrom are the passages of the run ctx belongs to: those a caller
// put on ctx, else the run's (shared with the graphs it calls, made on
// first use); nil outside a run.
func PassagesFrom(ctx context.Context) *Passages {
	if ctx == nil {
		return nil
	}
	if p, ok := ctx.Value(passagesKey{}).(*Passages); ok {
		return p
	}
	scope := cognition.ScopeOf(ctx)
	if scope == nil {
		return nil
	}
	return scope.Value(passagesKey{}, func() any { return &Passages{} }).(*Passages)
}

// Cite numbers segments as the run's next passages and keeps the new ones:
// one already numbered (same source and key) keeps its number.
func (p *Passages) Cite(segments []retrieval.Scored) []retrieval.Scored {
	if p == nil {
		return segments
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out, added := Cite(func(source, key string) (int, bool) {
		i := slices.IndexFunc(p.list, func(s retrieval.Scored) bool { return s.SourceID == source && s.Key == key })
		if i < 0 {
			return 0, false
		}
		return p.list[i].Number, true
	}, len(p.list), segments)
	p.list = append(p.list, added...)
	return out
}

// Add keeps passages already numbered.
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

// Cite is the one citer (forge 15b), shared by chat rounds and runs: it
// numbers segments after the count already cited. A segment known (its
// source and key cited before, or earlier in segments) keeps that number;
// a new one takes the next. It gives the segments numbered and the new
// ones, in order. segments is not changed.
func Cite(known func(sourceID, key string) (int, bool), count int, segments []retrieval.Scored) (numbered, added []retrieval.Scored) {
	numbered = make([]retrieval.Scored, len(segments))
	for i, s := range segments {
		if n, ok := known(s.SourceID, s.Key); ok && s.Key != "" {
			s.Number = n
		} else if j := slices.IndexFunc(added, func(a retrieval.Scored) bool { return s.Key != "" && a.SourceID == s.SourceID && a.Key == s.Key }); j >= 0 {
			s.Number = added[j].Number
		} else {
			s.Number = count + len(added) + 1
			added = append(added, s)
		}
		numbered[i] = s
	}
	return numbered, added
}
