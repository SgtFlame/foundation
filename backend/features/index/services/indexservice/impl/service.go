package impl

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

type service struct {
	root     string
	embedder embedservice.Service
	store    indexservice.Store
	events   volumeservice.FileEvents
	debounce time.Duration
	chunker  chunker

	queue   *queue
	indexMu sync.Mutex // one file is indexed at a time
}

// start runs the indexing worker and queues files the bus reports written.
func (s *service) start(ctx context.Context) {
	go s.queue.run(ctx, func(file string, force bool) {
		s.indexFile(features.BackgroundArgs(ctx), file, force) // a failure is logged by Fail
	})
	if s.events != nil {
		s.events.Subscribe(func(e volumeservice.FileEvent) {
			if indexable(e.File) {
				s.queue.touch(e.File)
			}
		})
	}
}

// indexable reports whether file is a markdown file, rather than a dotted
// one such as a sidecar or a temp file.
func indexable(file string) bool {
	base := filepath.Base(file)
	_, fileType, ok := kbservice.SplitFile(base)
	return ok && fileType == "markdown"
}

// volumeOf returns the ID of the volume holding file, or 0 outside a volume.
func (s *service) volumeOf(file string) uint64 {
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if volumeservice.IsVolume(dir) {
			marker, err := volumeservice.ReadMarker(dir)
			if err != nil {
				return 0
			}
			return marker.ID
		}
		if dir == s.root || dir == filepath.Dir(dir) {
			return 0
		}
	}
}

// rel is file relative to the root with '/' separators, the key both stores use.
func (s *service) rel(file string) string {
	return volumeservice.Rel(s.root, file)
}

func (s *service) abs(rel string) string {
	return filepath.Join(s.root, filepath.FromSlash(rel))
}

// folders splits a store path into the folders down to the file's name,
// without the extension, and the file's type.
func folders(rel string) ([]string, string) {
	parts := strings.Split(rel, "/")
	base, fileType, _ := kbservice.SplitFile(parts[len(parts)-1])
	parts[len(parts)-1] = base
	return parts, fileType
}

var log = logging.In("index")

func failed(resp features.ServiceResponse) *features.ServiceResponse {
	if resp.Code >= 300 {
		return &resp
	}
	return nil
}
