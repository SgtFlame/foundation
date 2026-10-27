package impl

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithRoot sets the absolute directory that volume paths are resolved against.
func WithRoot(root string) Option {
	return func(s *service) {
		s.root = root
	}
}

// WithFileEvents sets the bus that saved files are announced on.
func WithFileEvents(events volumeservice.FileEvents) Option {
	return func(s *service) {
		s.events = events
	}
}

// WithGracePeriod sets how long a room with no clients is kept.
func WithGracePeriod(d time.Duration) Option {
	return func(s *service) {
		s.gracePeriod = d
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton collabservice.Service. Options are only
// applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) collabservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{
		root:        defaultRoot(),
		gracePeriod: 30 * time.Second,
		rooms:       map[uint64]*room{},
		byPath:      map[string]*room{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// defaultRoot is ~/.foundation/kb, or the same under the temp directory when
// there is no home directory.
func defaultRoot() string {
	base, err := os.UserHomeDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, ".foundation", "kb")
}
