package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the application database, which holds the audit.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

// WithBootstrap sets the sealed start, whose Vault holds the server's secrets.
func WithBootstrap(boot *secrets.Bootstrap) Option {
	return func(s *service) {
		s.boot = boot
	}
}

// WithConfig sets the settings service, which stores the secret settings.
func WithConfig(config configservice.Service) Option {
	return func(s *service) {
		s.config = config
	}
}

// WithUserName names a user for UpdatedBy.
func WithUserName(name func(id uint64) string) Option {
	return func(s *service) {
		s.userName = name
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton secretsservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) secretsservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{userName: func(uint64) string { return "" }}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
