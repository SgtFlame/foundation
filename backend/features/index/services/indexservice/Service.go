package indexservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// Service keeps a search index of the knowledge base's volume files: each
// markdown file is split into overlapping chunks, which are embedded for
// semantic search and indexed for BM25 keyword search. Files are reindexed
// when the volume and collab services report them written.
type Service interface {
	IndexFile(args IndexFileArgs) IndexFileResponse
	Reindex(args ReindexArgs) ReindexResponse
	Find(args FindArgs) FindResponse
}

type IndexFileArgs struct {
	features.ServiceArgs
	Path kbservice.VolumePath // the folders down to the file's name, without its extension
	Type string               // content type, e.g. "markdown"
}

type IndexFileResponse struct {
	Common   features.ServiceResponse
	Chunks   int // chunks in the file
	Embedded int // chunks that needed a new embedding
}

type ReindexArgs struct {
	features.ServiceArgs
	Force bool // reindex every file, not only those changed since they were indexed
}

// ReindexResponse is returned once the files are queued; indexing continues
// in the background.
type ReindexResponse struct {
	Common features.ServiceResponse
	Queued int
}

type FindArgs struct {
	features.ServiceArgs
	Query     string
	Weight    float64  // 0..1: the share of the ranking given to vector search; keyword search gets 1 - Weight
	Count     int      // the most documents to return
	VolumeIDs []uint64 // optional; only search these volumes
}

type FindResponse struct {
	Common features.ServiceResponse
	Hits   []Hit // best first
}

// Hit is one matching document with its best chunks.
type Hit struct {
	VolumeID uint64
	Folders  []string // from the root down to the file's name, without its extension
	Type     string
	Score    float64
	Chunks   []ChunkRef // best first
}

// ChunkRef locates a chunk in its source file. Start/End and the lines are
// exact while the file is unchanged since it was indexed; Head and Tail find
// the chunk again after the file has drifted.
type ChunkRef struct {
	Start, End         int // byte offsets
	StartLine, EndLine int // 1-based, inclusive
	Heading            string
	Head, Tail         string
	Snippet            string
	Score              float64
	VectorRank         int // 1-based; 0 when the vector search did not find it
	KeywordRank        int // 1-based; 0 when the keyword search did not find it
}
