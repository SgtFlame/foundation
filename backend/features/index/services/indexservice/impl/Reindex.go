package impl

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// Reindex walks the root and queues every volume markdown file whose sidecar
// is missing or out of date (every one with args.Force). It also forgets the
// files that no longer exist, in the stores and their sidecars.
func (s *service) Reindex(args indexservice.ReindexArgs) indexservice.ReindexResponse {
	if s.embedder == nil || s.store == nil {
		return indexservice.ReindexResponse{Common: *args.Fail(http.StatusServiceUnavailable, errNotConfigured())}
	}
	desc := s.embedder.Describe(embedservice.DescribeArgs{ServiceArgs: args.ServiceArgs})
	if f := failed(desc.Common); f != nil {
		return indexservice.ReindexResponse{Common: *f}
	}
	ctx := args.Context()

	queued := 0
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip what cannot be read
		}
		if d.IsDir() {
			return nil
		}
		if source := indexservice.SourceFile(p); source != "" {
			if _, err := os.Stat(source); err != nil {
				_ = os.Remove(p) // an orphaned sidecar
			}
			return nil
		}
		if !indexable(p) || !volumeservice.InsideVolume(s.root, filepath.Dir(p)) {
			return nil
		}
		if args.Force || stale(p, desc) {
			s.queue.push(p, args.Force)
			queued++
		}
		return nil
	})
	if err != nil {
		return indexservice.ReindexResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}

	if err := s.forgetMissing(ctx); err != nil {
		log.Tags("reindex").Log(ctx, slog.LevelWarn, err, "cannot forget removed files")
	}
	return indexservice.ReindexResponse{Common: features.OK(), Queued: queued}
}

// stale reports whether file's sidecar does not describe its current content
// embedded by the current model.
func stale(file string, desc embedservice.DescribeResponse) bool {
	meta, err := indexservice.ReadMeta(file)
	if err != nil || meta.Version != indexservice.MetaVersion || meta.Model != desc.Model || meta.Dimensions != desc.Dimensions {
		return true
	}
	content, err := os.ReadFile(file)
	return err != nil || hashText(string(content)) != meta.FileHash
}

// forgetMissing removes the stored chunks of files that no longer exist.
func (s *service) forgetMissing(ctx context.Context) error {
	paths, err := s.store.Paths(ctx)
	if err != nil {
		return err
	}
	for _, rel := range paths {
		if _, err := os.Stat(s.abs(rel)); err == nil {
			continue
		}
		s.indexMu.Lock()
		err := s.removeFile(ctx, s.abs(rel))
		s.indexMu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
