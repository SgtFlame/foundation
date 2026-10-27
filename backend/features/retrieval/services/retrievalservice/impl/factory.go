package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the database holding the segment feedback.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

// WithEmbedder sets the service that embeds the query.
func WithEmbedder(embedder embedservice.Service) Option {
	return func(s *service) {
		s.embedder = embedder
	}
}

// WithConfig sets where the retrieval sections are read from.
func WithConfig(reader config.Reader) Option {
	return func(s *service) {
		s.config = reader
	}
}

// WithRegistry sets the registry holding the retrieval.source contributions.
func WithRegistry(reg *extensions.Registry) Option {
	return func(s *service) {
		s.registry = reg
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton retrievalservice.Service. Options are
// only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) retrievalservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{registry: extensions.NewRegistry()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
