package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/extensions/services/extensionsservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithRegistry sets the registry that is described.
func WithRegistry(registry *extensions.Registry) Option {
	return func(s *service) {
		s.registry = registry
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton extensionsservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) extensionsservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{registry: extensions.NewRegistry()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
