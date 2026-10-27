package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the database holding the entries.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

// WithStore sets the entries table retrieval searches; without one,
// entries are kept but not searchable.
func WithStore(store chronicle.Store) Option {
	return func(s *service) {
		s.store = store
	}
}

// WithEmbedder sets the service that embeds entries for vector search;
// without one (or without an API key), entries are found by keywords only.
func WithEmbedder(embedder embedservice.Service) Option {
	return func(s *service) {
		s.embedder = embedder
	}
}

// WithUserName sets how a share request names its owner for the
// moderators.
func WithUserName(name func(user uint64) string) Option {
	return func(s *service) {
		s.userName = name
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton chronicleservice.Service. Options
// are only applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) chronicleservice.Service {
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
