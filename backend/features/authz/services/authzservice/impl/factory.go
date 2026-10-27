package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithPolicy sets the policy whose grants and roles the service edits.
func WithPolicy(pol *authz.Policy) Option {
	return func(s *service) {
		s.pol = pol
	}
}

// WithMembers sets how the service tells whether a user belongs to a
// tenant: only members are granted roles there.
func WithMembers(member func(tenant, user uint64) (bool, error)) Option {
	return func(s *service) {
		s.member = member
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton authzservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) authzservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{member: func(uint64, uint64) (bool, error) { return true, nil }}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
