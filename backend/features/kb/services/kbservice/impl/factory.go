package impl

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithRoot sets the absolute directory that volume paths are resolved against.
func WithRoot(root string) Option {
	return func(s *service) {
		s.root = root
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton kbservice.Service. Options are only
// applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) kbservice.Service {
	once.Do(func() {
		instance = &service{root: defaultRoot()}
		for _, opt := range opts {
			opt(instance)
		}
	})
	return instance
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
