package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithBootstrap sets the sealed start the service reports and unseals.
func WithBootstrap(boot *secrets.Bootstrap) Option {
	return func(s *service) {
		s.boot = boot
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton bootstrapservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) bootstrapservice.Service {
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

type service struct {
	boot *secrets.Bootstrap
}
