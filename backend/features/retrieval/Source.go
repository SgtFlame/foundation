// Package retrieval is the context retrieval engine's shared types
// (documentation/ContextRetrieval.md): the retrieval.source point,
// the segments sources return, what a round injected, and the feedback that
// grounds later retrievals. retrievalservice runs the pipeline.
package retrieval

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Sources is the retrieval.source point: every place retrieval searches.
var Sources = extensions.NewPoint[Source](
	"retrieval.source",
	"Where retrieval searches for a chat round's context: Volumes (library) and Chronicle memory (chronicle)",
)

// Axis is the side of the Chronicle ↔ Library dial a source is on.
type Axis string

const (
	AxisChronicle Axis = "chronicle"
	AxisLibrary   Axis = "library"
)

// Source searches one kind of content both ways. Its searches get the
// call's context (features.CallFrom reads the principal back), and return
// at most k segments, best first. A source that fails is skipped for the
// retrieval.
type Source struct {
	extensions.Descriptor
	Title         string
	Axis          Axis
	VectorSearch  func(ctx context.Context, vector []float32, k int, filter Filter) ([]Segment, error)
	KeywordSearch func(ctx context.Context, query string, k int, filter Filter) ([]Segment, error)
}

// Filter narrows a search; the zero Filter searches everything.
type Filter struct {
	VolumeIDs []uint64
}

// Segment is one piece of content a source found.
type Segment struct {
	SourceID string
	ID       string // unique within the source, e.g. a chunk's path and ordinal
	Key      string // what feedback is counted against: a chunk's content hash, an entry or round ID
	Title    string // how a citation names it, e.g. "veritas/notes › Install"
	Text     string
	Location Location
}

// Location is where a citation opens: a Volume file's lines, or a Chronicle
// entry. For a library chunk the offsets and lines are exact while the file
// is unchanged since it was indexed.
type Location struct {
	EntryID   uint64 `json:"entryId,string,omitempty"` // a Chronicle entry
	VolumeID  uint64 `json:"volumeId,string,omitempty"`
	Path      string `json:"path,omitempty"` // relative to the knowledge base root, with '/' and the extension
	Start     int    `json:"start,omitempty"`
	End       int    `json:"end,omitempty"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	Heading   string `json:"heading,omitempty"`
}

// Scored is a segment as retrieval selected it, with every number that
// explains why: Score = RRF · AxisMult · Grounding.
type Scored struct {
	Segment
	Number      int      // its citation, [1]…[n]
	Keys        []string // every feedback key it covers, Key first: merged neighbours add theirs
	Also        []string // other paths holding the same text, cited once
	Score       float64
	RRF         float64
	AxisMult    float64
	Grounding   float64
	VectorRank  int // 1-based in its source; 0 when the vector search missed it
	KeywordRank int
	Pinned      bool
	Reason      string // why a pinned segment is in, e.g. "checked" or "stage 1"; empty for retrieval's own choice
	Tokens      int    // estimated
}
