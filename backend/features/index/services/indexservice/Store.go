package indexservice

import "context"

// Chunk is one indexed chunk as the stores keep it. Path is the file relative
// to the knowledge base root, with '/' separators and its extension.
type Chunk struct {
	VolumeID uint64
	Path     string
	Ordinal  int // position in the file, from 0
	Hash     string
	Text     string
	Vector   []float32
}

// Match is a search result, best first.
type Match struct {
	VolumeID uint64
	Path     string
	Ordinal  int
	Hash     string // of Text, as in the sidecar's MetaChunk
	Text     string
}

// Filter narrows a search; the zero Filter searches everything.
type Filter struct {
	VolumeIDs []uint64
}

// Store keeps the chunks, their embeddings for semantic search and their
// texts for BM25 keyword search (LanceDB in serve).
type Store interface {
	Dimensions() int
	// Replace makes chunks the whole content of path.
	Replace(ctx context.Context, path string, chunks []Chunk) error
	// Vectors returns path's stored embeddings by chunk hash.
	Vectors(ctx context.Context, path string) (map[string][]float32, error)
	Delete(ctx context.Context, path string) error
	Paths(ctx context.Context) ([]string, error)
	// VectorSearch returns the k chunks nearest to vector, best first.
	VectorSearch(ctx context.Context, vector []float32, k int, filter Filter) ([]Match, error)
	// KeywordSearch returns the k chunks best matching query by BM25, best first.
	KeywordSearch(ctx context.Context, query string, k int, filter Filter) ([]Match, error)
}
