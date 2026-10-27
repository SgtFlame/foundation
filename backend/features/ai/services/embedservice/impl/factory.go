package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithProvider sets the AI provider that embeds the texts.
func WithProvider(provider embedservice.Provider) Option {
	return func(s *service) {
		s.provider = provider
	}
}

// WithBatchSize caps how many texts go to the provider in one call, below the
// provider's own limit.
func WithBatchSize(n int) Option {
	return func(s *service) {
		s.batchSize = n
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton embedservice.Service. Options are only
// applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) embedservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{batchSize: 100}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
