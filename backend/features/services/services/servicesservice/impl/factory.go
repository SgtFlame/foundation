package impl

import (
	"context"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/services/services/servicesservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithProcesses sets where the announcements are read (servers.List in
// serve).
func WithProcesses(list func(ctx context.Context) ([]servicesservice.Process, error)) Option {
	return func(s *service) { s.list = list }
}

// WithKnown sets every service's name, so one no process serves is listed.
func WithKnown(names []string) Option {
	return func(s *service) { s.known = names }
}

var (
	once     sync.Once
	instance *service
)

// CreateService returns the singleton, built on the first call.
func CreateService(opts ...Option) servicesservice.Service {
	once.Do(func() { instance = newService(opts...) })
	return instance
}

// New makes a service apart from the singleton, for tests.
func New(opts ...Option) servicesservice.Service { return newService(opts...) }

func newService(opts ...Option) *service {
	s := &service{}
	for _, o := range opts {
		o(s)
	}
	return s
}

type service struct {
	list  func(ctx context.Context) ([]servicesservice.Process, error)
	known []string
}
