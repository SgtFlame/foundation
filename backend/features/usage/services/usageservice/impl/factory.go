package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the database holding the usage records.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton usageservice.Service. Options are only
// applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) usageservice.Service {
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
