package data

import (
	"context"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
)

// sandbox is the records' copy-on-write scope in a dry run (forge 06f):
// reads see the projections with the run's own events over them, and
// nothing is written. Its writes are not in the indexes, so its queries
// scan, up to the records section's scanLimit.
type sandbox struct {
	Projections
	dry *bus.Dry
}

func (s *sandbox) Get(ctx context.Context, tenant uint64, projector, key string) (live.Projected, bool, error) {
	return s.dry.Get(ctx, tenant, projector, key)
}

func (s *sandbox) Watch(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *live.Entry, error) {
	return s.dry.Watch(ctx, tenant, projector, pattern)
}

// WaitFor reads the value as the run has it: a dry run waits for nothing.
func (s *sandbox) WaitFor(ctx context.Context, tenant uint64, projector, key string, _ uint64, _ time.Duration) (live.Projected, error) {
	v, _, err := s.dry.Get(ctx, tenant, projector, key)
	return v, err
}

// For is the deps a read or a write for args uses: in a dry run (a
// context with bus.WithDry), the records' sandbox over the projections.
func (d Deps) For(args features.ServiceArgs) Deps {
	if dry := bus.DryOf(args.Context()); dry != nil && d.Projections != nil {
		if _, ok := d.Projections.(*sandbox); !ok {
			d.Projections = &sandbox{Projections: d.Projections, dry: dry}
		}
	}
	return d
}
