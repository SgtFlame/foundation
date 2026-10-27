// Package entrystore keeps Chronicle entries in LanceDB's entries table,
// beside the index's chunks: their embeddings for vector search and an FTS
// index on their text for BM25 keyword search. Like lancestore it needs CGO
// and LanceDB's native library: build with scripts/build.sh.
package entrystore

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"

	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

const table = "entries"

// ftsIndex is the name of the full-text index on the text column.
const ftsIndex = "text_fts"

// optimizeDelay is how long writes must pause before the FTS index is
// brought up to date with them.
const optimizeDelay = 10 * time.Second

var columns = []string{"entry_id", "hash", "title", "text"}

// Store implements chronicle.Store. IDs are kept as strings: LanceDB returns
// query results as JSON, whose numbers cannot hold a TSID. An entry that
// could not be embedded has a zero vector and embedded = false, so vector
// searches skip it and keyword searches still find it.
type Store struct {
	conn       contracts.IConnection
	table      contracts.ITable
	schema     *arrow.Schema
	dimensions int

	mu       sync.Mutex
	hasFTS   bool
	optimize *time.Timer
}

// Open opens (or creates) the entries table in the LanceDB database at dir.
// A table of another dimension is dropped, since its embeddings came from
// another model; chronicleservice.Reindex then embeds every entry again.
func Open(ctx context.Context, dir string, dimensions int) (*Store, error) {
	conn, err := lancedb.Connect(ctx, dir, nil)
	if err != nil {
		return nil, fmt.Errorf("entrystore: connect %s: %w", dir, err)
	}
	s := &Store{conn: conn, dimensions: dimensions, schema: arrow.NewSchema([]arrow.Field{
		{Name: "entry_id", Type: arrow.BinaryTypes.String},
		{Name: "tenant_id", Type: arrow.BinaryTypes.String},
		{Name: "owner_id", Type: arrow.BinaryTypes.String},
		{Name: "shared", Type: arrow.FixedWidthTypes.Boolean},
		{Name: "hash", Type: arrow.BinaryTypes.String},
		{Name: "title", Type: arrow.BinaryTypes.String},
		{Name: "text", Type: arrow.BinaryTypes.String},
		{Name: "embedded", Type: arrow.FixedWidthTypes.Boolean},
		{Name: "vector", Type: arrow.FixedSizeListOf(int32(dimensions), arrow.PrimitiveTypes.Float32)},
	}, nil)}
	if err := s.openTable(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	if err := s.ensureFTS(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Hashes are the stored entries' hashes by entry ID, so chronicleservice
// can tell which entries a dropped or lost table no longer holds.
func (s *Store) Hashes(ctx context.Context) (map[uint64]string, error) {
	rows, err := s.table.Select(ctx, contracts.QueryConfig{Columns: []string{"entry_id", "hash", "embedded"}})
	if err != nil {
		return nil, fmt.Errorf("entrystore: hashes: %w", err)
	}
	out := make(map[uint64]string, len(rows))
	for _, row := range rows {
		id, _ := strconv.ParseUint(str(row["entry_id"]), 10, 64)
		if embedded, _ := row["embedded"].(bool); id != 0 && embedded {
			out[id] = str(row["hash"])
		}
	}
	return out, nil
}

// ensureFTS creates the full-text index once the table has rows; LanceDB
// cannot build one on an empty table.
func (s *Store) ensureFTS(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasFTS {
		return nil
	}
	indexes, err := s.table.GetAllIndexes(ctx)
	if err != nil {
		return fmt.Errorf("entrystore: indexes: %w", err)
	}
	for _, index := range indexes {
		if index.Name == ftsIndex {
			s.hasFTS = true
			return nil
		}
	}
	if n, err := s.table.Count(ctx); err != nil || n == 0 {
		return err
	}
	stem, positions := true, false
	params := contracts.IndexParams{FtsLanguage: "English", FtsStem: &stem, FtsWithPosition: &positions}
	opts := &contracts.CreateIndexOptions{Name: ftsIndex, Replace: true, WaitTimeout: time.Minute}
	if err := s.table.CreateIndexWithParams(ctx, []string{"text"}, contracts.IndexTypeFts, params, opts); err != nil {
		return fmt.Errorf("entrystore: create FTS index: %w", err)
	}
	s.hasFTS = true
	return nil
}

// written schedules folding new rows into the indexes once writes pause.
func (s *Store) written() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.optimize != nil {
		s.optimize.Reset(optimizeDelay)
		return
	}
	s.optimize = time.AfterFunc(optimizeDelay, func() {
		s.mu.Lock()
		s.optimize = nil
		s.mu.Unlock()
		if _, err := s.table.Optimize(context.Background()); err != nil {
			logging.In("chronicle").Tags("entrystore").Log(context.Background(), slog.LevelWarn, err, "optimize failed")
		}
	})
}

func (s *Store) openTable(ctx context.Context) error {
	names, err := s.conn.TableNames(ctx)
	if err != nil {
		return fmt.Errorf("entrystore: %w", err)
	}
	for _, name := range names {
		if name != table {
			continue
		}
		t, err := s.conn.OpenTable(ctx, table)
		if err != nil {
			return fmt.Errorf("entrystore: open %s: %w", table, err)
		}
		if schema, err := t.Schema(ctx); err == nil && schema.Equal(s.schema) {
			s.table = t
			return nil
		}
		t.Close()
		if err := s.conn.DropTable(ctx, table); err != nil {
			return fmt.Errorf("entrystore: drop %s: %w", table, err)
		}
	}
	schema, err := lancedb.NewSchema(s.schema)
	if err != nil {
		return fmt.Errorf("entrystore: %w", err)
	}
	if s.table, err = s.conn.CreateTable(ctx, table, schema); err != nil {
		return fmt.Errorf("entrystore: create %s: %w", table, err)
	}
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	if s.optimize != nil {
		s.optimize.Stop()
	}
	s.mu.Unlock()
	s.table.Close()
	return s.conn.Close()
}

func (s *Store) Dimensions() int {
	return s.dimensions
}

func (s *Store) Put(ctx context.Context, row chronicle.Row) error {
	if row.Vector != nil && len(row.Vector) != s.dimensions {
		return fmt.Errorf("entrystore: entry %d has %d dimensions, not %d", row.EntryID, len(row.Vector), s.dimensions)
	}
	if err := s.Delete(ctx, row.EntryID); err != nil {
		return err
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), s.schema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(id(row.EntryID))
	b.Field(1).(*array.StringBuilder).Append(id(row.TenantID))
	b.Field(2).(*array.StringBuilder).Append(id(row.OwnerID))
	b.Field(3).(*array.BooleanBuilder).Append(row.Shared)
	b.Field(4).(*array.StringBuilder).Append(row.Hash)
	b.Field(5).(*array.StringBuilder).Append(row.Title)
	b.Field(6).(*array.StringBuilder).Append(row.Text)
	b.Field(7).(*array.BooleanBuilder).Append(row.Vector != nil)
	vectors := b.Field(8).(*array.FixedSizeListBuilder)
	vectors.Append(true)
	vector := row.Vector
	if vector == nil {
		vector = make([]float32, s.dimensions)
	}
	vectors.ValueBuilder().(*array.Float32Builder).AppendValues(vector, nil)
	record := b.NewRecord()
	defer record.Release()
	if err := s.table.AddRecords(ctx, []arrow.Record{record}, nil); err != nil {
		return fmt.Errorf("entrystore: add entry %d: %w", row.EntryID, err)
	}
	if err := s.ensureFTS(ctx); err != nil {
		return err
	}
	s.written()
	return nil
}

func (s *Store) Delete(ctx context.Context, entryID uint64) error {
	if err := s.table.Delete(ctx, "entry_id = "+quote(id(entryID))); err != nil {
		return fmt.Errorf("entrystore: delete entry %d: %w", entryID, err)
	}
	return nil
}

// VectorSearch returns the owner's k nearest embedded entries. Embeddings
// are unit length, so the L2 ordering LanceDB uses is the cosine ordering.
func (s *Store) VectorSearch(ctx context.Context, owner chronicle.Owner, vector []float32, k int) ([]chronicle.Match, error) {
	rows, err := s.table.Select(ctx, contracts.QueryConfig{
		Columns:      columns,
		VectorSearch: &contracts.VectorSearch{Column: "vector", Vector: vector, K: k},
		Where:        where(owner) + " AND embedded = true",
		Limit:        &k,
	})
	if err != nil {
		return nil, fmt.Errorf("entrystore: vector search: %w", err)
	}
	return matches(rows), nil
}

// KeywordSearch returns the owner's k entries whose text best matches query
// by BM25, through the FTS index.
func (s *Store) KeywordSearch(ctx context.Context, owner chronicle.Owner, query string, k int) ([]chronicle.Match, error) {
	s.mu.Lock()
	hasFTS := s.hasFTS
	s.mu.Unlock()
	if !hasFTS || strings.TrimSpace(query) == "" {
		return nil, nil // nothing stored yet
	}
	rows, err := s.table.Select(ctx, contracts.QueryConfig{
		Columns:   columns,
		FTSSearch: &contracts.FTSSearch{Column: "text", Query: query},
		Where:     where(owner),
		Limit:     &k,
	})
	if err != nil {
		return nil, fmt.Errorf("entrystore: keyword search: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool { return score(rows[i]) > score(rows[j]) })
	return matches(rows), nil
}

func score(row map[string]any) float64 {
	f, _ := row["_score"].(float64)
	return f
}

// where keeps the owner's rows and the tenant's shared ones (only others',
// with OthersOnly).
func where(owner chronicle.Owner) string {
	tenant := "tenant_id = " + quote(id(owner.TenantID))
	if owner.OthersOnly {
		return tenant + " AND shared = true AND owner_id <> " + quote(id(owner.UserID))
	}
	return tenant + " AND (owner_id = " + quote(id(owner.UserID)) + " OR shared = true)"
}

func matches(rows []map[string]any) []chronicle.Match {
	out := make([]chronicle.Match, 0, len(rows))
	for _, row := range rows {
		entryID, _ := strconv.ParseUint(str(row["entry_id"]), 10, 64)
		out = append(out, chronicle.Match{EntryID: entryID, Hash: str(row["hash"]), Title: str(row["title"]), Text: str(row["text"])})
	}
	return out
}

func id(v uint64) string {
	return strconv.FormatUint(v, 10)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// quote makes s an SQL string literal for a LanceDB filter.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
