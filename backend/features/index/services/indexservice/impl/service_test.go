package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// fakeEmbedder embeds a text as its counts of a few words, so texts sharing
// words are close.
type fakeEmbedder struct {
	mu       sync.Mutex
	model    string
	embedded []string
}

var fakeWords = []string{"apple", "banana", "cherry", "date"}

func (f *fakeEmbedder) Embed(args embedservice.EmbedArgs) embedservice.EmbedResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	resp := embedservice.EmbedResponse{Common: features.ServiceResponse{Code: http.StatusOK}, Model: f.model, Dimensions: len(fakeWords)}
	for _, text := range args.Texts {
		if args.Purpose == embedservice.PurposeDocument {
			f.embedded = append(f.embedded, text)
		}
		v := make([]float32, len(fakeWords))
		for i, w := range fakeWords {
			v[i] = float32(strings.Count(strings.ToLower(text), w))
		}
		resp.Vectors = append(resp.Vectors, v)
	}
	return resp
}

func (f *fakeEmbedder) Describe(embedservice.DescribeArgs) embedservice.DescribeResponse {
	return embedservice.DescribeResponse{Common: features.ServiceResponse{Code: http.StatusOK}, Model: f.model, Dimensions: len(fakeWords), MaxBatch: 10}
}

func (f *fakeEmbedder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.embedded)
}

// memStore is a Store in memory: vector search by dot product, keyword
// search by term count.
type memStore struct {
	mu     sync.Mutex
	chunks map[string][]indexservice.Chunk
}

func newMemStore() *memStore { return &memStore{chunks: map[string][]indexservice.Chunk{}} }

func (m *memStore) Dimensions() int { return len(fakeWords) }

func (m *memStore) Replace(_ context.Context, path string, chunks []indexservice.Chunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks[path] = slices.Clone(chunks)
	return nil
}

func (m *memStore) Vectors(_ context.Context, path string) (map[string][]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]float32{}
	for _, c := range m.chunks[path] {
		out[c.Hash] = c.Vector
	}
	return out, nil
}

func (m *memStore) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.chunks, path)
	return nil
}

func (m *memStore) Paths(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for p := range m.chunks {
		out = append(out, p)
	}
	return out, nil
}

func (m *memStore) rank(score func(indexservice.Chunk) float64, k int, filter indexservice.Filter) []indexservice.Match {
	m.mu.Lock()
	defer m.mu.Unlock()
	type sc struct {
		c indexservice.Chunk
		s float64
	}
	var all []sc
	for _, chunks := range m.chunks {
		for _, c := range chunks {
			if len(filter.VolumeIDs) > 0 && !slices.Contains(filter.VolumeIDs, c.VolumeID) {
				continue
			}
			if s := score(c); s > 0 {
				all = append(all, sc{c, s})
			}
		}
	}
	slices.SortStableFunc(all, func(a, b sc) int {
		if a.s != b.s {
			if a.s > b.s {
				return -1
			}
			return 1
		}
		return strings.Compare(a.c.Path, b.c.Path)
	})
	var out []indexservice.Match
	for _, x := range all[:min(k, len(all))] {
		out = append(out, indexservice.Match{VolumeID: x.c.VolumeID, Path: x.c.Path, Ordinal: x.c.Ordinal, Hash: x.c.Hash, Text: x.c.Text})
	}
	return out
}

func (v *memStore) VectorSearch(_ context.Context, vector []float32, k int, filter indexservice.Filter) ([]indexservice.Match, error) {
	return v.rank(func(c indexservice.Chunk) float64 {
		var dot float64
		for i := range vector {
			dot += float64(vector[i] * c.Vector[i])
		}
		return dot
	}, k, filter), nil
}

func (s *memStore) KeywordSearch(_ context.Context, query string, k int, filter indexservice.Filter) ([]indexservice.Match, error) {
	terms := queryTerms(query)
	return s.rank(func(c indexservice.Chunk) float64 {
		n := 0
		for _, t := range terms {
			n += strings.Count(strings.ToLower(c.Text), t)
		}
		return float64(n)
	}, k, filter), nil
}

type fixture struct {
	root     string
	svc      *service
	embedder *fakeEmbedder
	store    *memStore
}

func newFixture(t *testing.T, opts ...Option) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{root: root, embedder: &fakeEmbedder{model: "fake-1"}, store: newMemStore()}
	opts = append([]Option{WithRoot(root), WithEmbedder(f.embedder), WithStore(f.store), WithChunkSize(400), WithOverlap(64)}, opts...)
	f.svc = newService(opts...)
	f.volume(t, "vol", 7)
	return f
}

func (f *fixture) volume(t *testing.T, name string, id uint64) {
	t.Helper()
	dir := filepath.Join(f.root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(volumeservice.Marker{ID: id})
	if err := os.WriteFile(filepath.Join(dir, volumeservice.MarkerFile), marker, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) write(t *testing.T, rel, content string) string {
	t.Helper()
	file := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func (f *fixture) index(t *testing.T, folders ...string) indexservice.IndexFileResponse {
	t.Helper()
	resp := f.svc.IndexFile(indexservice.IndexFileArgs{Path: *kbservice.NewVolumePath().Folder(folders...), Type: "markdown"})
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("IndexFile %v: %d %s", folders, resp.Common.Code, resp.Common.Message)
	}
	return resp
}

// filler pads a section past minSection, so sections are not merged.
const filler = "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
	"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure."

const fruitDoc = "# Apples\n\napple pie and apple tart. " + filler +
	"\n\n# Bananas\n\nbanana bread with banana split. " + filler +
	"\n\n# Cherries\n\ncherry jam and cherry cola. " + filler + "\n"

func TestIndexFileReusesUnchangedChunks(t *testing.T) {
	f := newFixture(t)
	file := f.write(t, "vol/fruit.md", fruitDoc)

	first := f.index(t, "vol", "fruit")
	if first.Chunks != 3 || first.Embedded != 3 {
		t.Fatalf("first index: %+v", first)
	}
	meta, err := indexservice.ReadMeta(file)
	if err != nil || len(meta.Chunks) != 3 || meta.Model != "fake-1" || meta.Chunks[1].Heading != "Bananas" {
		t.Fatalf("meta %+v, %v", meta, err)
	}
	if c := meta.Chunks[2]; fruitDoc[c.Start:c.End] == "" || !strings.HasPrefix(fruitDoc[c.Start:c.End], "# Cherries") || c.StartLine != 9 {
		t.Fatalf("cherry chunk at %+v", c)
	}

	// Editing one section re-embeds only its chunk
	f.write(t, "vol/fruit.md", strings.Replace(fruitDoc, "banana split", "banana smoothie", 1))
	second := f.index(t, "vol", "fruit")
	if second.Embedded != 1 || f.embedder.count() != 4 {
		t.Fatalf("after an edit: %+v, %d embedded in all", second, f.embedder.count())
	}

	// A new model invalidates everything
	f.embedder.model = "fake-2"
	if third := f.index(t, "vol", "fruit"); third.Embedded != 3 {
		t.Fatalf("after a model change: %+v", third)
	}
}

func TestIndexFileErrors(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, "loose.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		folders []string
		code    uint16
	}{
		"outside a volume": {[]string{"loose"}, http.StatusBadRequest},
		"missing":          {[]string{"vol", "nope"}, http.StatusNotFound},
	}
	for name, c := range cases {
		resp := f.svc.IndexFile(indexservice.IndexFileArgs{Path: *kbservice.NewVolumePath().Folder(c.folders...), Type: "markdown"})
		if resp.Common.Code != c.code {
			t.Errorf("%s: %d %s", name, resp.Common.Code, resp.Common.Message)
		}
	}
}

func TestFindFusesAndGroups(t *testing.T) {
	f := newFixture(t)
	f.volume(t, "other", 9)
	f.write(t, "vol/fruit.md", fruitDoc)
	f.write(t, "vol/dates.md", "# Dates\n\ndate palms grow dates. A date is sweet.\n")
	f.write(t, "other/cherry.md", "# Cherry\n\ncherry cherry cherry orchard.\n")
	f.index(t, "vol", "fruit")
	f.index(t, "vol", "dates")
	f.index(t, "other", "cherry")

	find := func(query string, weight float64, count int, volumes ...uint64) []indexservice.Hit {
		t.Helper()
		resp := f.svc.Find(indexservice.FindArgs{Query: query, Weight: weight, Count: count, VolumeIDs: volumes})
		if resp.Common.Code != http.StatusOK {
			t.Fatalf("Find %q: %d %s", query, resp.Common.Code, resp.Common.Message)
		}
		return resp.Hits
	}

	hits := find("cherry", 0.5, 10)
	if len(hits) != 2 || strings.Join(hits[0].Folders, "/") != "other/cherry" || hits[0].VolumeID != 9 {
		t.Fatalf("cherry hits %+v", hits)
	}
	c := hits[1].Chunks[0]
	if c.Heading != "Cherries" || c.VectorRank == 0 || c.KeywordRank == 0 || !strings.Contains(c.Snippet, "cherry") || c.Start == 0 {
		t.Fatalf("fruit's cherry chunk %+v", c)
	}

	// One hit per document, at most count
	if hits := find("apple banana cherry date", 1, 2); len(hits) != 2 {
		t.Fatalf("count 2 gave %d hits", len(hits))
	}
	// Keyword only: the vector store is not consulted, so no vector ranks
	for _, h := range find("dolor", 0, 10) {
		if h.Chunks[0].VectorRank != 0 {
			t.Fatalf("keyword-only hit has a vector rank: %+v", h)
		}
	}
	// Volume filter
	if hits := find("cherry", 0.5, 10, 7); len(hits) != 1 || hits[0].VolumeID != 7 {
		t.Fatalf("filtered hits %+v", hits)
	}

	for name, args := range map[string]indexservice.FindArgs{
		"empty":      {Query: " ", Weight: 0.5},
		"weight":     {Query: "x", Weight: 1.5},
		"count high": {Query: "x", Count: 1000},
	} {
		if code := f.svc.Find(args).Common.Code; code != http.StatusBadRequest {
			t.Errorf("%s: code %d", name, code)
		}
	}
}

func TestReindexAndEvents(t *testing.T) {
	events := volumeservice.NewFileEvents()
	f := newFixture(t, WithFileEvents(events), WithDebounce(20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.start(ctx)

	a := f.write(t, "vol/a.md", "apple notes\n")
	f.write(t, "vol/b.md", "banana notes\n")
	if resp := f.svc.Reindex(indexservice.ReindexArgs{}); resp.Queued != 2 {
		t.Fatalf("Reindex queued %d", resp.Queued)
	}
	waitFor(t, "both files indexed", func() bool { return len(must(f.store.Paths(ctx))) == 2 })
	if resp := f.svc.Reindex(indexservice.ReindexArgs{}); resp.Queued != 0 {
		t.Fatalf("an up-to-date index queued %d", resp.Queued)
	}

	// A saved file is reindexed after the debounce
	f.write(t, "vol/a.md", "apple apple notes\n")
	events.Publish(volumeservice.FileEvent{Op: volumeservice.FileChanged, File: a})
	events.Publish(volumeservice.FileEvent{Op: volumeservice.FileChanged, File: indexservice.MetaFile(a)}) // ignored
	waitFor(t, "a.md reindexed", func() bool {
		meta, err := indexservice.ReadMeta(a)
		return err == nil && meta.FileHash == hashText("apple apple notes\n")
	})

	// A removed file is forgotten, with its sidecar
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	f.svc.Reindex(indexservice.ReindexArgs{})
	if paths := must(f.store.Paths(ctx)); len(paths) != 1 || paths[0] != "vol/b.md" {
		t.Fatalf("keyword paths %v", paths)
	}
	if _, err := os.Stat(indexservice.MetaFile(a)); !os.IsNotExist(err) {
		t.Fatalf("sidecar of a removed file: %v", err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
