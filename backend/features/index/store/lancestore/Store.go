// Package lancestore keeps index chunks in LanceDB: their embeddings for
// vector search and an FTS index on their text for BM25 keyword search. It
// needs CGO and LanceDB's native library: build with scripts/build.sh.
package lancestore

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

	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

const table = "chunks"

// ftsIndex is the name of the full-text index on the text column.
const ftsIndex = "text_fts"

// optimizeDelay is how long writes must pause before the FTS index is
// brought up to date with them.
const optimizeDelay = 10 * time.Second

// Store implements indexservice.Store. Volume IDs are kept as strings:
// LanceDB returns query results as JSON, whose numbers cannot hold a TSID.
type Store struct {
	conn       contracts.IConnection
	table      contracts.ITable
	schema     *arrow.Schema
	dimensions int

	mu       sync.Mutex
	hasFTS   bool
	optimize *time.Timer
}

// Open opens (or creates) the chunk table in the LanceDB database at dir. A
// table of another dimension is dropped, since its embeddings came from
// another model; the sidecars then no longer match and every file is
// embedded again.
func Open(ctx context.Context, dir string, dimensions int) (*Store, error) {
	conn, err := lancedb.Connect(ctx, dir, nil)
	if err != nil {
		return nil, fmt.Errorf("lancestore: connect %s: %w", dir, err)
	}
	s := &Store{conn: conn, dimensions: dimensions, schema: arrow.NewSchema([]arrow.Field{
		{Name: "volume_id", Type: arrow.BinaryTypes.String},
		{Name: "path", Type: arrow.BinaryTypes.String},
		{Name: "ordinal", Type: arrow.PrimitiveTypes.Int32},
		{Name: "hash", Type: arrow.BinaryTypes.String},
		{Name: "text", Type: arrow.BinaryTypes.String},
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
		return fmt.Errorf("lancestore: indexes: %w", err)
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
		return fmt.Errorf("lancestore: create FTS index: %w", err)
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
			logging.In("index").Tags("lancestore").Log(context.Background(), slog.LevelWarn, err, "optimize failed")
		}
	})
}

func (s *Store) openTable(ctx context.Context) error {
	names, err := s.conn.TableNames(ctx)
	if err != nil {
		return fmt.Errorf("lancestore: %w", err)
	}
	for _, name := range names {
		if name != table {
			continue
		}
		t, err := s.conn.OpenTable(ctx, table)
		if err != nil {
			return fmt.Errorf("lancestore: open %s: %w", table, err)
		}
		if schema, err := t.Schema(ctx); err == nil && schema.Equal(s.schema) {
			s.table = t
			return nil
		}
		t.Close()
		if err := s.conn.DropTable(ctx, table); err != nil {
			return fmt.Errorf("lancestore: drop %s: %w", table, err)
		}
	}
	schema, err := lancedb.NewSchema(s.schema)
	if err != nil {
		return fmt.Errorf("lancestore: %w", err)
	}
	if s.table, err = s.conn.CreateTable(ctx, table, schema); err != nil {
		return fmt.Errorf("lancestore: create %s: %w", table, err)
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

func (s *Store) Replace(ctx context.Context, path string, chunks []indexservice.Chunk) error {
	if err := s.Delete(ctx, path); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}
	record, err := s.record(chunks)
	if err != nil {
		return err
	}
	defer record.Release()
	if err := s.table.AddRecords(ctx, []arrow.Record{record}, nil); err != nil {
		return fmt.Errorf("lancestore: add %s: %w", path, err)
	}
	if err := s.ensureFTS(ctx); err != nil {
		return err
	}
	s.written()
	return nil
}

func (s *Store) record(chunks []indexservice.Chunk) (arrow.Record, error) {
	b := array.NewRecordBuilder(memory.NewGoAllocator(), s.schema)
	defer b.Release()
	vectors := b.Field(5).(*array.FixedSizeListBuilder)
	values := vectors.ValueBuilder().(*array.Float32Builder)
	for _, c := range chunks {
		if len(c.Vector) != s.dimensions {
			return nil, fmt.Errorf("lancestore: chunk %d of %s has %d dimensions, not %d", c.Ordinal, c.Path, len(c.Vector), s.dimensions)
		}
		b.Field(0).(*array.StringBuilder).Append(strconv.FormatUint(c.VolumeID, 10))
		b.Field(1).(*array.StringBuilder).Append(c.Path)
		b.Field(2).(*array.Int32Builder).Append(int32(c.Ordinal))
		b.Field(3).(*array.StringBuilder).Append(c.Hash)
		b.Field(4).(*array.StringBuilder).Append(c.Text)
		vectors.Append(true)
		values.AppendValues(c.Vector, nil)
	}
	return b.NewRecord(), nil
}

func (s *Store) Vectors(ctx context.Context, path string) (map[string][]float32, error) {
	rows, err := s.table.Select(ctx, contracts.QueryConfig{Columns: []string{"hash", "vector"}, Where: "path = " + quote(path)})
	if err != nil {
		return nil, fmt.Errorf("lancestore: vectors of %s: %w", path, err)
	}
	out := make(map[string][]float32, len(rows))
	for _, row := range rows {
		hash, _ := row["hash"].(string)
		values, _ := row["vector"].([]any)
		if hash == "" || len(values) != s.dimensions {
			continue
		}
		v := make([]float32, len(values))
		for i, x := range values {
			f, _ := x.(float64)
			v[i] = float32(f)
		}
		out[hash] = v
	}
	return out, nil
}

func (s *Store) Delete(ctx context.Context, path string) error {
	if err := s.table.Delete(ctx, "path = "+quote(path)); err != nil {
		return fmt.Errorf("lancestore: delete %s: %w", path, err)
	}
	return nil
}

func (s *Store) Paths(ctx context.Context) ([]string, error) {
	rows, err := s.table.Select(ctx, contracts.QueryConfig{Columns: []string{"path"}})
	if err != nil {
		return nil, fmt.Errorf("lancestore: paths: %w", err)
	}
	seen := map[string]bool{}
	var paths []string
	for _, row := range rows {
		if p, _ := row["path"].(string); p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// VectorSearch returns the k nearest chunks. Embeddings are unit length, so
// the L2 ordering LanceDB uses is the cosine ordering.
func (s *Store) VectorSearch(ctx context.Context, vector []float32, k int, filter indexservice.Filter) ([]indexservice.Match, error) {
	rows, err := s.table.Select(ctx, contracts.QueryConfig{
		Columns:      []string{"volume_id", "path", "ordinal", "hash", "text"},
		VectorSearch: &contracts.VectorSearch{Column: "vector", Vector: vector, K: k},
		Where:        where(filter),
		Limit:        &k,
	})
	if err != nil {
		return nil, fmt.Errorf("lancestore: vector search: %w", err)
	}
	return matches(rows), nil
}

// KeywordSearch returns the k chunks whose text best matches query by BM25,
// through the FTS index. Rows written since the last optimize are searched
// too, without the index.
func (s *Store) KeywordSearch(ctx context.Context, query string, k int, filter indexservice.Filter) ([]indexservice.Match, error) {
	s.mu.Lock()
	hasFTS := s.hasFTS
	s.mu.Unlock()
	if !hasFTS || strings.TrimSpace(query) == "" {
		return nil, nil // nothing indexed yet
	}
	rows, err := s.table.Select(ctx, contracts.QueryConfig{
		Columns:   []string{"volume_id", "path", "ordinal", "hash", "text"},
		FTSSearch: &contracts.FTSSearch{Column: "text", Query: query},
		Where:     where(filter),
		Limit:     &k,
	})
	if err != nil {
		return nil, fmt.Errorf("lancestore: keyword search: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool { return score(rows[i]) > score(rows[j]) })
	return matches(rows), nil
}

func score(row map[string]any) float64 {
	f, _ := row["_score"].(float64)
	return f
}

// where is filter as a LanceDB SQL condition, "" for no condition.
func where(filter indexservice.Filter) string {
	if len(filter.VolumeIDs) == 0 {
		return ""
	}
	ids := make([]string, len(filter.VolumeIDs))
	for i, id := range filter.VolumeIDs {
		ids[i] = quote(strconv.FormatUint(id, 10))
	}
	return "volume_id IN (" + strings.Join(ids, ", ") + ")"
}

func matches(rows []map[string]any) []indexservice.Match {
	matches := make([]indexservice.Match, 0, len(rows))
	for _, row := range rows {
		volumeID, _ := strconv.ParseUint(str(row["volume_id"]), 10, 64)
		ordinal, _ := row["ordinal"].(float64)
		matches = append(matches, indexservice.Match{VolumeID: volumeID, Path: str(row["path"]), Ordinal: int(ordinal), Hash: str(row["hash"]), Text: str(row["text"])})
	}
	return matches
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// quote makes s an SQL string literal for a LanceDB filter.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
