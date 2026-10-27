package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the database holding the conversations.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

// WithChat sets the chat service that generates the replies.
func WithChat(chat chatservice.Service) Option {
	return func(s *service) {
		s.chat = chat
	}
}

// WithConfig sets where the chat section, the levels, the model catalog and
// the context defaults are read from.
func WithConfig(reader config.Reader) Option {
	return func(s *service) {
		s.config = reader
	}
}

// WithRegistry sets the registry holding the chat.contextProvider contributions.
func WithRegistry(reg *extensions.Registry) Option {
	return func(s *service) {
		s.registry = reg
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton conversationservice.Service. Options
// are only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) conversationservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{registry: extensions.NewRegistry(), waiting: map[string]*waiting{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
