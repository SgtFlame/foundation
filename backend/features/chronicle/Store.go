package chronicle

import "context"

// Row is one entry as the entries table keeps it.
type Row struct {
	EntryID  uint64
	TenantID uint64
	OwnerID  uint64
	Shared   bool // other members may find it (and everyone, in the global tenant)
	Hash     string
	Title    string
	Text     string
	Vector   []float32 // nil when the entry could not be embedded: it is found by keywords only
}

// Match is a search result, best first.
type Match struct {
	EntryID uint64
	Hash    string
	Title   string
	Text    string
}

// Owner narrows a search to one user's entries and those shared with them
// in that tenant; OthersOnly keeps only the shared entries of others (the
// global tenant, which holds copies of the user's own).
type Owner struct {
	TenantID   uint64
	UserID     uint64
	OthersOnly bool
}

// Store keeps the active entries' text for keyword (BM25) search and their
// embeddings for vector search (LanceDB's entries table in serve, beside
// the index's chunks).
type Store interface {
	Dimensions() int
	// Put makes row the entry's whole content.
	Put(ctx context.Context, row Row) error
	Delete(ctx context.Context, entryID uint64) error
	// Hashes are the hashes of the embedded entries, by entry ID.
	Hashes(ctx context.Context) (map[uint64]string, error)
	// VectorSearch returns the owner's k entries nearest to vector, best first.
	VectorSearch(ctx context.Context, owner Owner, vector []float32, k int) ([]Match, error)
	// KeywordSearch returns the owner's k entries best matching query by BM25, best first.
	KeywordSearch(ctx context.Context, owner Owner, query string, k int) ([]Match, error)
}
