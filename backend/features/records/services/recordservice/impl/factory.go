package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithRecords sets what the records live on: the bus that decides their
// commands, the subscriptions that answer their queries, and their deps.
func WithRecords(b *bus.Bus, subs *subscribe.Subscriptions, deps data.Deps) Option {
	return func(s *service) {
		s.bus, s.subs, s.deps = b, subs, deps
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton recordservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) recordservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// New makes a service that is not the singleton, for tests and tools.
func New(opts ...Option) recordservice.Service { return newService(opts...) }
