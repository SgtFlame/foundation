package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
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

// WithAuthenticator sets the request authenticator: its signer issues the
// tokens, and setup ends its single-user mode.
func WithAuthenticator(a *identity.Authenticator) Option {
	return func(s *service) {
		s.auth = a
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton identityservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) identityservice.Service {
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
	s.sessions = identity.Sessions{DB: s.db, Signer: s.auth.Signer()}
	return s
}
