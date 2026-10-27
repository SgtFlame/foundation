package impl

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithVictoria sets the storage the server's records go to: its target is
// where queries go, and browser records are queued on it.
func WithVictoria(v *logging.Victoria) Option {
	return func(s *service) {
		s.victoria = v
	}
}

// WithConfig sets where the logging section is read from.
func WithConfig(r config.Reader) Option {
	return func(s *service) {
		s.config = r
	}
}

// WithDB sets the application database, holding the users' own levels.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton logsservice.Service. Options are only
// applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) logsservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{
		client: &http.Client{Timeout: 30 * time.Second},
		tail:   &http.Client{}, // a tail lasts as long as its request
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.loadUserLevels(context.Background()); err != nil {
		log.Error(context.Background(), err, "cannot load the users' log levels")
	}
	return s
}
