package impl

import (
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval/fuse"
)

// chunksPerHit is how many chunks a document's hit carries.
const chunksPerHit = 3

// scored is one chunk with its fused score.
type scored = fuse.Ranked[indexservice.Match]

// doc is one document's fused chunks, best first.
type doc struct {
	path   string
	score  float64
	chunks []scored
}

// fuseMatches merges the two rankings by weighted Reciprocal Rank Fusion
// (features/retrieval/fuse), a chunk being its path and ordinal.
func fuseMatches(vector, keyword []indexservice.Match, weight float64) []scored {
	return fuse.Fuse(vector, keyword, func(m indexservice.Match) [2]any { return [2]any{m.Path, m.Ordinal} }, weight, fuse.DefaultK)
}

// group collects fused chunks into documents scored by their best chunk and
// returns the best count of them.
func group(chunks []scored, count int) []doc {
	byPath := map[string]*doc{}
	var docs []*doc
	for _, c := range chunks { // best first, so a document's first chunk is its best
		d, ok := byPath[c.Item.Path]
		if !ok {
			d = &doc{path: c.Item.Path, score: c.Score}
			byPath[c.Item.Path] = d
			docs = append(docs, d)
		}
		if len(d.chunks) < chunksPerHit {
			d.chunks = append(d.chunks, c)
		}
	}
	out := make([]doc, 0, min(count, len(docs)))
	for _, d := range docs[:min(count, len(docs))] {
		out = append(out, *d)
	}
	return out
}
