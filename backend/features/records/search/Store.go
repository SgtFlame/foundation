// Package search is the records' full-text and vector search (Forge
// 03-DataModel question 3, Records.md › Built in slice 24): a LanceDB
// table per tenant, beside the tenant's other tables, holding each
// searchable record's text with an FTS index and, when it could be
// embedded, its vector. The indexer follows the rec stream and keeps it;
// searches fuse the keyword and vector rankings with retrieval's fuse.
// Like lancestore it needs CGO and LanceDB's native library: build with
// scripts/build.sh.
package search

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"

	"github.com/SgtFlame/foundation/backend/features/records"
)

var log = records.Log.Tags("search")

const table = "records"

// ftsIndex is the name of the full-text index on the text column.
const ftsIndex = "text_fts"

// optimizeDelay is how long writes must pause before the FTS index is
// brought up to date with them; until then LanceDB searches the new rows
// without it.
const optimizeDelay = 10 * time.Second

var columns = []string{"key", "entity", "record_id", "hash"}

// Row is one record as searched.
type Row struct {
	Entity   string
	RecordID string
	Hash     string // of the text, so an unchanged text is not embedded again
	Title    string
	Text     string
	Vector   []float32 // nil when it could not be embedded
}

// Key is a row's key, as the records projection keys the record.
func (r Row) Key() string { return r.Entity + "." + r.RecordID }

// Match is a row a search found.
type Match struct {
	Entity   string
	RecordID string
	Title    string
	Text     string
}

// Store is one tenant's records table. An unembedded row has a zero
// vector and embedded = false, so vector searches skip it and keyword
// searches still find it.
type Store struct {
	conn       contracts.IConnection
	table      contracts.ITable
	schema     *arrow.Schema
	dimensions int

	mu       sync.Mutex
	hasFTS   bool
	optimize *time.Timer
}

// Open opens (or creates) the records table in the LanceDB database at
// dir. A table of another shape or dimension is dropped; the indexer then
// indexes every record again.
func Open(ctx context.Context, dir string, dimensions int) (*Store, error) {
	conn, err := lancedb.Connect(ctx, dir, nil)
	if err != nil {
		return nil, fmt.Errorf("search: connect %s: %w", dir, err)
	}
	s := &Store{conn: conn, dimensions: dimensions, schema: arrow.NewSchema([]arrow.Field{
		{Name: "key", Type: arrow.BinaryTypes.String},
		{Name: "entity", Type: arrow.BinaryTypes.String},
		{Name: "record_id", Type: arrow.BinaryTypes.String},
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

func (s *Store) openTable(ctx context.Context) error {
	names, err := s.conn.TableNames(ctx)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	for _, name := range names {
		if name != table {
			continue
		}
		t, err := s.conn.OpenTable(ctx, table)
		if err != nil {
			return fmt.Errorf("search: open %s: %w", table, err)
		}
		if schema, err := t.Schema(ctx); err == nil && schema.Equal(s.schema) {
			s.table = t
			return nil
		}
		t.Close()
		if err := s.conn.DropTable(ctx, table); err != nil {
			return fmt.Errorf("search: drop %s: %w", table, err)
		}
	}
	schema, err := lancedb.NewSchema(s.schema)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if s.table, err = s.conn.CreateTable(ctx, table, schema); err != nil {
		return fmt.Errorf("search: create %s: %w", table, err)
	}
	return nil
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
		return fmt.Errorf("search: indexes: %w", err)
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
		return fmt.Errorf("search: create FTS index: %w", err)
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
			log.Log(context.Background(), slog.LevelWarn, err, "optimize failed")
		}
	})
}

// Close closes the table.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.optimize != nil {
		s.optimize.Stop()
	}
	s.mu.Unlock()
	s.table.Close()
	return s.conn.Close()
}

// Hash is a row's text hash; "" when there is no row or it was not
// embedded while it could be.
func (s *Store) Hash(ctx context.Context, key string, embedded bool) (string, error) {
	where := "key = " + quote(key)
	if embedded {
		where += " AND embedded = true"
	}
	one := 1
	rows, err := s.table.Select(ctx, contracts.QueryConfig{Columns: columns, Where: where, Limit: &one})
	if err != nil {
		return "", fmt.Errorf("search: hash: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return str(rows[0]["hash"]), nil
}

// Put replaces a record's row.
func (s *Store) Put(ctx context.Context, row Row) error {
	if row.Vector != nil && len(row.Vector) != s.dimensions {
		return fmt.Errorf("search: %s has %d dimensions, not %d", row.Key(), len(row.Vector), s.dimensions)
	}
	if err := s.Delete(ctx, row.Key()); err != nil {
		return err
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), s.schema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(row.Key())
	b.Field(1).(*array.StringBuilder).Append(row.Entity)
	b.Field(2).(*array.StringBuilder).Append(row.RecordID)
	b.Field(3).(*array.StringBuilder).Append(row.Hash)
	b.Field(4).(*array.StringBuilder).Append(row.Title)
	b.Field(5).(*array.StringBuilder).Append(row.Text)
	b.Field(6).(*array.BooleanBuilder).Append(row.Vector != nil)
	vectors := b.Field(7).(*array.FixedSizeListBuilder)
	vectors.Append(true)
	vector := row.Vector
	if vector == nil {
		vector = make([]float32, s.dimensions)
	}
	vectors.ValueBuilder().(*array.Float32Builder).AppendValues(vector, nil)
	record := b.NewRecord()
	defer record.Release()
	if err := s.table.AddRecords(ctx, []arrow.Record{record}, nil); err != nil {
		return fmt.Errorf("search: add %s: %w", row.Key(), err)
	}
	if err := s.ensureFTS(ctx); err != nil {
		return err
	}
	s.written()
	return nil
}

// Delete removes a record's row.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.table.Delete(ctx, "key = "+quote(key)); err != nil {
		return fmt.Errorf("search: delete %s: %w", key, err)
	}
	return nil
}

// DeleteEntity removes every row of an entity type.
func (s *Store) DeleteEntity(ctx context.Context, entity string) error {
	if err := s.table.Delete(ctx, "entity = "+quote(entity)); err != nil {
		return fmt.Errorf("search: delete %s: %w", entity, err)
	}
	return nil
}

// VectorSearch is the k nearest embedded rows, in entity when it is set.
// Embeddings are unit length, so the L2 ordering is the cosine ordering.
func (s *Store) VectorSearch(ctx context.Context, entity string, vector []float32, k int) ([]Match, error) {
	rows, err := s.table.Select(ctx, contracts.QueryConfig{
		Columns:      append(columns, "title", "text"),
		VectorSearch: &contracts.VectorSearch{Column: "vector", Vector: vector, K: k},
		Where:        where(entity, true),
		Limit:        &k,
	})
	if err != nil {
		return nil, fmt.Errorf("search: vector search: %w", err)
	}
	return matches(rows), nil
}

// KeywordSearch is the k rows whose text best matches query by BM25, in
// entity when it is set.
func (s *Store) KeywordSearch(ctx context.Context, entity, query string, k int) ([]Match, error) {
	s.mu.Lock()
	hasFTS := s.hasFTS
	s.mu.Unlock()
	if !hasFTS || strings.TrimSpace(query) == "" {
		return nil, nil // nothing stored yet
	}
	cfg := contracts.QueryConfig{
		Columns:   append(columns, "title", "text"),
		FTSSearch: &contracts.FTSSearch{Column: "text", Query: query},
		Limit:     &k,
	}
	if w := where(entity, false); w != "" {
		cfg.Where = w
	}
	rows, err := s.table.Select(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("search: keyword search: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool { return score(rows[i]) > score(rows[j]) })
	return matches(rows), nil
}

func where(entity string, embedded bool) string {
	var parts []string
	if entity != "" {
		parts = append(parts, "entity = "+quote(entity))
	}
	if embedded {
		parts = append(parts, "embedded = true")
	}
	return strings.Join(parts, " AND ")
}

func score(row map[string]any) float64 {
	f, _ := row["_score"].(float64)
	return f
}

func matches(rows []map[string]any) []Match {
	out := make([]Match, 0, len(rows))
	for _, row := range rows {
		out = append(out, Match{Entity: str(row["entity"]), RecordID: str(row["record_id"]), Title: str(row["title"]), Text: str(row["text"])})
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// quote makes s an SQL string literal for a LanceDB filter.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
