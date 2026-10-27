// Package indexsource contributes the index's chunks to retrieval as the
// library source (documentation/ContextRetrieval.md): Volume text,
// on the Library side of the Chronicle ↔ Library dial.
package indexsource

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// ID names the source in settings, round segments and feedback.
const ID = "library"

// Contribute registers the library source, searching store; root is the
// knowledge base directory, whose sidecars give each chunk's position.
func Contribute(reg *extensions.Registry, store indexservice.Store, root string) {
	src := source{root: root}
	retrieval.Sources.Register(reg, retrieval.Source{
		Descriptor: extensions.Descriptor{ID: ID, Order: 200, Source: "index"},
		Title:      "Volumes",
		Axis:       retrieval.AxisLibrary,
		VectorSearch: func(ctx context.Context, vector []float32, k int, f retrieval.Filter) ([]retrieval.Segment, error) {
			if !hasVolumes(ctx) {
				return nil, nil
			}
			matches, err := store.VectorSearch(ctx, vector, k, indexservice.Filter{VolumeIDs: f.VolumeIDs})
			return src.segments(readable(ctx, matches)), err
		},
		KeywordSearch: func(ctx context.Context, query string, k int, f retrieval.Filter) ([]retrieval.Segment, error) {
			if !hasVolumes(ctx) {
				return nil, nil
			}
			matches, err := store.KeywordSearch(ctx, query, k, indexservice.Filter{VolumeIDs: f.VolumeIDs})
			return src.segments(readable(ctx, matches)), err
		},
	})
}

// hasVolumes tells whether the call's tenant has the Volumes
// (kbservice.InTenant); a call naming no principal is the default tenant's.
func hasVolumes(ctx context.Context) bool {
	p, _, ok := features.CallFrom(ctx)
	return !ok || kbservice.InTenant(p)
}

// readable keeps the matches in Volumes the call's principal may read.
func readable(ctx context.Context, matches []indexservice.Match) []indexservice.Match {
	p, _, ok := features.CallFrom(ctx)
	if !ok {
		return matches
	}
	may := map[uint64]bool{}
	out := matches[:0]
	for _, m := range matches {
		allowed, seen := may[m.VolumeID]
		if !seen {
			res := authz.Tenant(p) + "/" + authz.Resource("volume", m.VolumeID)
			allowed = authz.Check(ctx, p, "volume.read", res).Allowed
			may[m.VolumeID] = allowed
		}
		if allowed {
			out = append(out, m)
		}
	}
	return out
}

type source struct {
	root string
}

// segments turns matches into segments, locating each chunk from its
// file's sidecar; positions are best effort.
func (s source) segments(matches []indexservice.Match) []retrieval.Segment {
	metas := map[string]indexservice.Meta{}
	out := make([]retrieval.Segment, 0, len(matches))
	for _, m := range matches {
		meta, ok := metas[m.Path]
		if !ok {
			meta, _ = indexservice.ReadMeta(filepath.Join(s.root, filepath.FromSlash(m.Path)))
			metas[m.Path] = meta
		}
		loc := retrieval.Location{VolumeID: m.VolumeID, Path: m.Path}
		if m.Ordinal < len(meta.Chunks) && meta.Chunks[m.Ordinal].Hash == m.Hash {
			c := meta.Chunks[m.Ordinal]
			loc.Start, loc.End, loc.StartLine, loc.EndLine, loc.Heading = c.Start, c.End, c.StartLine, c.EndLine, c.Heading
		}
		out = append(out, retrieval.Segment{
			SourceID: ID,
			ID:       m.Path + "#" + strconv.Itoa(m.Ordinal),
			Key:      m.Hash,
			Title:    title(m.Path, loc.Heading),
			Text:     m.Text,
			Location: loc,
		})
	}
	return out
}

// title is the file's folders and name without its extension, then its
// heading: "veritas/notes › Install".
func title(path, heading string) string {
	dir, base := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir, base = path[:i+1], path[i+1:]
	}
	if name, _, ok := kbservice.SplitFile(base); ok {
		base = name
	}
	if heading == "" {
		return dir + base
	}
	return dir + base + " › " + strings.ReplaceAll(heading, " > ", " › ")
}
