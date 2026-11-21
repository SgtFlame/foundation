package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDeps sets what the service works on, made after the registry: the
// projections, the tenant database, the bus its imports send commands
// through, the subscriptions it reads records with and the registry.
func WithDeps(deps func() Deps) Option {
	return func(s *service) {
		s.deps = deps
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton appservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) appservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

// New makes a service apart from the singleton, for tests.
func New(opts ...Option) appservice.Service { return newService(opts...) }

func newService(opts ...Option) *service {
	s := &service{deps: func() Deps { return Deps{} }}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
