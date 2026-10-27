package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the application database, holding identity's tables.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton tenantservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) tenantservice.Service {
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
