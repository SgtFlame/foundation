package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/config"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithProvider sets the AI provider that completes the chats.
func WithProvider(provider chatservice.Provider) Option {
	return func(s *service) {
		s.provider = provider
	}
}

// WithConfig sets the settings the chat levels and the model catalog are
// read from; without it, a call naming a level fails with 503.
func WithConfig(reader config.Reader) Option {
	return func(s *service) {
		s.config = reader
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton chatservice.Service. Options are only
// applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) chatservice.Service {
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
