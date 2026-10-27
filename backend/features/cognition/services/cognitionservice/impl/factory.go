package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the database holding the runs and their traces.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

// WithEngine sets the engine whose graphs and node types are served.
func WithEngine(e *engine.Engine) Option {
	return func(s *service) {
		s.engine = e
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton cognitionservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) cognitionservice.Service {
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
