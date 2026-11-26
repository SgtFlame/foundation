// Package retrievaltest runs the real retrieval engine in tests without
// LanceDB (forge 15a): in-memory stores behind the real library and
// chronicle sources, keyword search only (no embedder, so the vector
// search is skipped), and the retrieval sections' defaults. kb.search over
// it is the real tool, held to the real access checks.
package retrievaltest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	chroniclesource "github.com/SgtFlame/foundation/backend/features/chronicle/source"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	indexsource "github.com/SgtFlame/foundation/backend/features/index/source"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	retrievalimpl "github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice/impl"
	retrievalsettings "github.com/SgtFlame/foundation/backend/features/retrieval/settings"
	"gorm.io/gorm"
)

// Stores are the in-memory library and Chronicle memory a test fills.
type Stores struct {
	Library *Library
	Memory  *Memory
}

// Contribute registers the real library and chronicle sources over new
// in-memory stores; root is the knowledge base root (Volume markers and
// sidecars are read from it).
func Contribute(reg *extensions.Registry, root string) Stores {
	s := Stores{Library: &Library{}, Memory: &Memory{}}
	indexsource.Contribute(reg, s.Library, root)
	chroniclesource.Contribute(reg, s.Memory)
	return s
}

// Service is a real retrieval service over reg's sources, with the
// retrieval sections' defaults; gdb (optional) holds segment feedback.
func Service(reg *extensions.Registry, gdb *gorm.DB) retrievalservice.Service {
	opts := []retrievalimpl.Option{retrievalimpl.WithConfig(Config{}), retrievalimpl.WithRegistry(reg)}
	if gdb != nil {
		opts = append(opts, retrievalimpl.WithDB(gdb))
	}
	return retrievalimpl.New(opts...)
}

// Config resolves the retrieval sections to their defaults.
type Config struct{}

func (Config) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	switch section {
	case retrievalsettings.ID:
		return json.RawMessage(`{"rrfK": 60, "candidateFactor": 5, "minCandidates": 50, "axisBase": 4, "defaultTopK": 8, "budgetShare": 0.5, "maxBudgetTokens": 10000}`), nil
	case retrievalsettings.FeedbackID:
		return json.RawMessage(`{"gamma": 0.5, "min": 0.25, "max": 4}`), nil
	}
	return nil, fmt.Errorf("no section %q in the test configuration", section)
}

// words are text's lower-case words of three letters or more.
func words(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

// score counts the query's distinct words found in text.
func score(query, text string) int {
	have := map[string]bool{}
	for _, w := range words(text) {
		have[w] = true
	}
	n, seen := 0, map[string]bool{}
	for _, w := range words(query) {
		if have[w] && !seen[w] {
			n++
		}
		seen[w] = true
	}
	return n
}

// Library is an in-memory indexservice.Store: one chunk per Add.
type Library struct {
	mu     sync.Mutex
	chunks []indexservice.Match
}

// Add indexes text as a chunk of path (relative to the root, with its
// extension) in the Volume volume.
func (l *Library) Add(volume uint64, path, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.chunks {
		if c.Path == path {
			n++
		}
	}
	l.chunks = append(l.chunks, indexservice.Match{VolumeID: volume, Path: path, Ordinal: n, Hash: hash(text), Text: text})
}

// AddFile writes text as the file path (relative to root) under a heading
// starting at line, with its index sidecar, and indexes it as one chunk,
// so its passage carries the heading and lines as a real index's does.
func (l *Library) AddFile(root string, volume uint64, path, heading string, line int, text string) error {
	file := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	body := strings.Repeat("\n", max(line-1, 0)) + text + "\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		return err
	}
	lines := strings.Count(text, "\n") + 1
	meta := indexservice.Meta{Version: 1, Chunks: []indexservice.MetaChunk{{Hash: hash(text), Start: line - 1, End: line - 1 + len(text),
		StartLine: line, EndLine: line + lines - 1, Heading: heading}}}
	if err := indexservice.WriteMeta(file, meta); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.chunks = append(l.chunks, indexservice.Match{VolumeID: volume, Path: path, Ordinal: 0, Hash: hash(text), Text: text})
	return nil
}

// hash is a chunk's hash as the index keeps it: SHA-256's first 128 bits.
func hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

func (l *Library) Dimensions() int { return 0 }
func (l *Library) Replace(context.Context, string, []indexservice.Chunk) error {
	return nil
}
func (l *Library) Vectors(context.Context, string) (map[string][]float32, error) { return nil, nil }
func (l *Library) Delete(context.Context, string) error                          { return nil }
func (l *Library) Paths(context.Context) ([]string, error)                       { return nil, nil }
func (l *Library) VectorSearch(context.Context, []float32, int, indexservice.Filter) ([]indexservice.Match, error) {
	return nil, nil
}

func (l *Library) KeywordSearch(_ context.Context, query string, k int, f indexservice.Filter) ([]indexservice.Match, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	type hit struct {
		m indexservice.Match
		n int
	}
	var hits []hit
	for _, c := range l.chunks {
		if (len(f.VolumeIDs) > 0 && !contains(f.VolumeIDs, c.VolumeID)) || contains(f.ExceptVolumeIDs, c.VolumeID) {
			continue
		}
		if n := score(query, c.Text); n > 0 {
			hits = append(hits, hit{c, n})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	out := make([]indexservice.Match, 0, min(k, len(hits)))
	for _, h := range hits {
		if len(out) == k {
			break
		}
		out = append(out, h.m)
	}
	return out, nil
}

func contains(ids []uint64, id uint64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Memory is an in-memory chronicle.Store.
type Memory struct {
	mu   sync.Mutex
	rows []chronicle.Row
}

func (m *Memory) Dimensions() int { return 0 }

func (m *Memory) Put(_ context.Context, row chronicle.Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rows {
		if r.EntryID == row.EntryID {
			m.rows[i] = row
			return nil
		}
	}
	m.rows = append(m.rows, row)
	return nil
}

func (m *Memory) Delete(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rows {
		if r.EntryID == id {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *Memory) Hashes(context.Context) (map[uint64]string, error) { return nil, nil }

func (m *Memory) VectorSearch(context.Context, chronicle.Owner, []float32, int) ([]chronicle.Match, error) {
	return nil, nil
}

// KeywordSearch finds the owner's entries and those shared with them, as
// the LanceDB table does: in the owner's tenant, their own or shared;
// OthersOnly keeps the shared entries of others.
func (m *Memory) KeywordSearch(_ context.Context, o chronicle.Owner, query string, k int) ([]chronicle.Match, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	type hit struct {
		r chronicle.Row
		n int
	}
	var hits []hit
	for _, r := range m.rows {
		mine := r.OwnerID == o.UserID
		if r.TenantID != o.TenantID || (o.OthersOnly && (mine || !r.Shared)) || (!o.OthersOnly && !mine && !r.Shared) {
			continue
		}
		if n := score(query, r.Title+" "+r.Text); n > 0 {
			hits = append(hits, hit{r, n})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	out := make([]chronicle.Match, 0, min(k, len(hits)))
	for _, h := range hits {
		if len(out) == k {
			break
		}
		out = append(out, chronicle.Match{EntryID: h.r.EntryID, Hash: h.r.Hash, Title: h.r.Title, Text: h.r.Text})
	}
	return out, nil
}
