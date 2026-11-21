package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithPrefabs sets what the prefabs live on: the tenant database holding
// the published versions, and the projections the versions follower runs
// on.
func WithPrefabs(deps prefabs.Deps) Option {
	return func(s *service) {
		s.deps = deps
	}
}

// WithRegistry sets the registry the catalog of primitives comes from,
// for Preview.
func WithRegistry(reg *extensions.Registry) Option {
	return func(s *service) {
		s.reg = reg
	}
}

// WithSubscriptions sets the queries Preview reads prefabs and their data
// through.
func WithSubscriptions(subs *subscribe.Subscriptions) Option {
	return func(s *service) {
		s.subs = subs
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton prefabservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) prefabservice.Service {
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
