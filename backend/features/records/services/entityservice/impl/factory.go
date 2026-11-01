package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/records/services/entityservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithEntities sets what the entity types live on: the tenant database
// holding the published versions, and the projections the versions
// follower runs on.
func WithEntities(deps entities.Deps) Option {
	return func(s *service) {
		s.deps = deps
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton entityservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) entityservice.Service {
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
