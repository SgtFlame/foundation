package impl

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// IndexFile indexes the volume file at args.Path now, re-embedding only the
// chunks that changed since it was last indexed.
func (s *service) IndexFile(args indexservice.IndexFileArgs) indexservice.IndexFileResponse {
	if args.Type != "markdown" {
		return indexservice.IndexFileResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("unsupported_file_type").Errorf("cannot index file type %q", args.Type))}
	}
	path := args.Path
	path.Type(args.Type)
	file, failure := volumeservice.Resolve(args.ServiceArgs, s.root, path, true)
	if failure != nil {
		return indexservice.IndexFileResponse{Common: *failure}
	}
	chunks, embedded, failure := s.indexFile(args.ServiceArgs, file, true)
	if failure != nil {
		return indexservice.IndexFileResponse{Common: *failure}
	}
	return indexservice.IndexFileResponse{Common: features.OK(), Chunks: chunks, Embedded: embedded}
}

// indexFile brings the stores and the sidecar of file up to date. Unless
// force is set, a file whose content and model match its sidecar is skipped.
// The sidecar is written last, so a failure part way leaves it stale and the
// file is indexed again later.
func (s *service) indexFile(args features.ServiceArgs, file string, force bool) (chunks, embedded int, failure *features.ServiceResponse) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	if s.embedder == nil || s.store == nil {
		return 0, 0, args.Fail(http.StatusServiceUnavailable, errNotConfigured())
	}
	ctx := args.Context()
	content, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		if err := s.removeFile(ctx, file); err != nil {
			return 0, 0, args.Fail(http.StatusInternalServerError, log.Wrap(err))
		}
		return 0, 0, args.Fail(http.StatusNotFound, log.Code("file_not_found").Errorf("%q does not exist", s.rel(file)))
	}
	if err != nil {
		return 0, 0, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	volumeID := s.volumeOf(file)
	if volumeID == 0 {
		return 0, 0, args.Fail(http.StatusBadRequest, log.Code("not_in_volume").Errorf("%q is not inside a volume", s.rel(file)))
	}
	desc := s.embedder.Describe(embedservice.DescribeArgs{ServiceArgs: args})
	if f := failed(desc.Common); f != nil {
		return 0, 0, f
	}
	if desc.Dimensions != s.store.Dimensions() {
		return 0, 0, args.Fail(http.StatusInternalServerError, log.Code("dimension_mismatch").Errorf("the embedding model makes %d dimensions but the vector store holds %d", desc.Dimensions, s.store.Dimensions()))
	}

	text := string(content)
	fileHash := hashText(text)
	meta, metaErr := indexservice.ReadMeta(file)
	sameModel := metaErr == nil && meta.Version == indexservice.MetaVersion && meta.Model == desc.Model && meta.Dimensions == desc.Dimensions
	if !force && sameModel && meta.FileHash == fileHash {
		return len(meta.Chunks), 0, nil
	}

	rel := s.rel(file)
	cut := s.chunker.split(text)
	vectors := map[string][]float32{}
	if sameModel {
		if vectors, err = s.store.Vectors(ctx, rel); err != nil {
			return 0, 0, args.Fail(http.StatusInternalServerError, log.Wrap(err))
		}
	}
	var missing []string
	for _, c := range cut {
		if _, ok := vectors[c.Hash]; !ok {
			vectors[c.Hash] = nil // once per distinct text
			missing = append(missing, c.Text)
		}
	}
	if len(missing) > 0 {
		embedArgs := args
		embedArgs.Meta = args.Meta.WithFeature("embed-index")
		resp := s.embedder.Embed(embedservice.EmbedArgs{
			ServiceArgs: embedArgs,
			Texts:       missing,
			Purpose:     embedservice.PurposeDocument,
			Title:       strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)),
		})
		if f := failed(resp.Common); f != nil {
			return 0, 0, f
		}
		for i, v := range resp.Vectors {
			vectors[hashText(missing[i])] = v
		}
	}

	rows := make([]indexservice.Chunk, len(cut))
	metaChunks := make([]indexservice.MetaChunk, len(cut))
	for i, c := range cut {
		rows[i] = indexservice.Chunk{VolumeID: volumeID, Path: rel, Ordinal: i, Hash: c.Hash, Text: c.Text, Vector: vectors[c.Hash]}
		metaChunks[i] = indexservice.MetaChunk{
			Hash: c.Hash, Start: c.Start, End: c.End, StartLine: c.StartLine, EndLine: c.EndLine,
			Heading: c.Heading, Head: c.Head, Tail: c.Tail,
		}
	}
	if err := s.store.Replace(ctx, rel, rows); err != nil {
		return 0, 0, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	meta = indexservice.Meta{
		Version: indexservice.MetaVersion, Model: desc.Model, Dimensions: desc.Dimensions,
		FileHash: fileHash, IndexedAt: time.Now().UTC(), Chunks: metaChunks,
	}
	if err := indexservice.WriteMeta(file, meta); err != nil {
		return 0, 0, args.Fail(http.StatusInternalServerError, log.Wrapf(err, "cannot write the index of %q", rel))
	}
	return len(cut), len(missing), nil
}

// removeFile forgets a file that no longer exists.
func (s *service) removeFile(ctx context.Context, file string) error {
	if err := s.store.Delete(ctx, s.rel(file)); err != nil {
		return err
	}
	if err := os.Remove(indexservice.MetaFile(file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
