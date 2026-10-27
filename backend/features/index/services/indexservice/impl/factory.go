package impl

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithRoot sets the absolute knowledge base directory that is indexed.
func WithRoot(root string) Option {
	return func(s *service) {
		s.root = root
	}
}

// WithEmbedder sets the service that embeds chunks and queries.
func WithEmbedder(embedder embedservice.Service) Option {
	return func(s *service) {
		s.embedder = embedder
	}
}

// WithStore sets where chunks are kept and searched (LanceDB in serve).
func WithStore(store indexservice.Store) Option {
	return func(s *service) {
		s.store = store
	}
}

// WithFileEvents sets the bus that announces written volume files.
func WithFileEvents(events volumeservice.FileEvents) Option {
	return func(s *service) {
		s.events = events
	}
}

// WithDebounce sets how long a file must stay unchanged before it is
// reindexed; collab autosaves every second while someone types.
func WithDebounce(d time.Duration) Option {
	return func(s *service) {
		s.debounce = d
	}
}

// WithChunkSize sets the most bytes in a chunk.
func WithChunkSize(n int) Option {
	return func(s *service) {
		s.chunker.size = n
	}
}

// WithOverlap sets how many bytes each chunk repeats from the one before.
func WithOverlap(n int) Option {
	return func(s *service) {
		s.chunker.overlap = n
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton indexservice.Service. Options are only
// applied by the first call; later calls return the existing instance. The
// first call starts the indexing worker, subscribes to file events and
// queues every file changed since it was last indexed.
func CreateService(opts ...Option) indexservice.Service {
	once.Do(func() {
		instance = newService(opts...)
		instance.start(context.Background())
		go instance.Reindex(indexservice.ReindexArgs{ServiceArgs: features.BackgroundArgs(context.Background())})
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{
		root:     defaultRoot(),
		debounce: 5 * time.Second,
		chunker:  chunker{size: 4096, overlap: 512},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.queue = newQueue(s.debounce)
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
