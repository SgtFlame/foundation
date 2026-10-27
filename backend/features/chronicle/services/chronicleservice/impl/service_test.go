package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// memStore keeps rows in memory.
type memStore struct {
	mu   sync.Mutex
	rows map[uint64]chronicle.Row
}

func (m *memStore) Dimensions() int { return 2 }
func (m *memStore) Put(_ context.Context, row chronicle.Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[row.EntryID] = row
	return nil
}
func (m *memStore) Delete(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
	return nil
}
func (m *memStore) Hashes(context.Context) (map[uint64]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[uint64]string{}
	for id, r := range m.rows {
		if r.Vector != nil {
			out[id] = r.Hash
		}
	}
	return out, nil
}
func (m *memStore) VectorSearch(context.Context, chronicle.Owner, []float32, int) ([]chronicle.Match, error) {
	return nil, nil
}
func (m *memStore) KeywordSearch(context.Context, chronicle.Owner, string, int) ([]chronicle.Match, error) {
	return nil, nil
}

// fakeEmbedder embeds every text as (1, 0), or fails while off.
type fakeEmbedder struct {
	off bool
}

func (f *fakeEmbedder) Embed(args embedservice.EmbedArgs) embedservice.EmbedResponse {
	if f.off {
		return embedservice.EmbedResponse{Common: features.ServiceResponse{Code: http.StatusServiceUnavailable}}
	}
	vectors := make([][]float32, len(args.Texts))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}
	return embedservice.EmbedResponse{Common: features.OK(), Vectors: vectors}
}

func (f *fakeEmbedder) Describe(embedservice.DescribeArgs) embedservice.DescribeResponse {
	return embedservice.DescribeResponse{Common: features.OK()}
}

func newTestService(t *testing.T) (*service, *memStore, *fakeEmbedder) {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, append(chronicle.Models(), conversation.Models()...)...); err != nil {
		t.Fatal(err)
	}
	store, embedder := &memStore{rows: map[uint64]chronicle.Row{}}, &fakeEmbedder{}
	return newService(WithDB(gdb), WithStore(store), WithEmbedder(embedder)), store, embedder
}

func me() features.ServiceArgs {
	return features.ServiceArgs{Principal: features.LocalPrincipal()}
}

func create(t *testing.T, s *service, in chronicleservice.EntryInput) chronicle.Entry {
	t.Helper()
	resp := s.Create(chronicleservice.CreateArgs{ServiceArgs: me(), EntryInput: in})
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("create %+v: %+v", in, resp.Common)
	}
	return resp.Entry
}

func TestEntriesAreEditedAndIndexed(t *testing.T) {
	s, store, embedder := newTestService(t)
	ada := create(t, s, chronicleservice.EntryInput{Type: "entity", Title: " Ada Lovelace ", ShortSummary: "A mathematician.", Fields: json.RawMessage(`{"kind": "person"}`)})
	engine := create(t, s, chronicleservice.EntryInput{Type: "note", Title: "Analytical engine", ParentID: ada.ID})
	s.indexing.Wait()
	if ada.Title != "Ada Lovelace" || ada.Status != chronicle.StatusActive || len(store.rows) != 2 || store.rows[ada.ID].Vector == nil {
		t.Fatalf("created %+v, rows %+v", ada, store.rows)
	}
	var stored chronicle.Entry
	s.db.First(&stored, ada.ID)
	if stored.IndexedHash != stored.Hash() {
		t.Errorf("indexed hash %q, want %q", stored.IndexedHash, stored.Hash())
	}

	// Validation: types, fields, parents and cycles
	for name, in := range map[string]chronicleservice.EntryInput{
		"unknown type": {Type: "nope", Title: "x"},
		"no title":     {Type: "note", Title: " "},
		"bad field":    {Type: "entity", Title: "x", Fields: json.RawMessage(`{"kind": "robot"}`)},
		"no parent":    {Type: "note", Title: "x", ParentID: 12345},
	} {
		if code := s.Create(chronicleservice.CreateArgs{ServiceArgs: me(), EntryInput: in}).Common.Code; code != http.StatusBadRequest {
			t.Errorf("%s: code %d", name, code)
		}
	}
	cycle := s.Update(chronicleservice.UpdateArgs{ServiceArgs: me(), ID: ada.ID, EntryInput: chronicleservice.EntryInput{Type: "entity", Title: "Ada", ParentID: engine.ID}})
	if cycle.Common.Code != http.StatusBadRequest {
		t.Errorf("cycle: code %d", cycle.Common.Code)
	}

	// A type change drops the old type's fields; an edit is indexed again
	embedder.off = true
	moved := s.Update(chronicleservice.UpdateArgs{ServiceArgs: me(), ID: ada.ID, EntryInput: chronicleservice.EntryInput{Type: "note", Title: "Ada", ShortSummary: "Changed.", Fields: json.RawMessage(`{"kind": "person"}`)}})
	s.indexing.Wait()
	if moved.Common.Code != http.StatusOK || moved.Entry.Fields != "{}" || store.rows[ada.ID].Vector != nil || store.rows[ada.ID].Title != "Ada" {
		t.Fatalf("moved %+v, row %+v", moved, store.rows[ada.ID])
	}
	// Reindex embeds what could not be embedded before
	embedder.off = false
	if resp := s.Reindex(chronicleservice.ReindexArgs{ServiceArgs: features.BackgroundArgs(context.Background())}); resp.Indexed != 1 || store.rows[ada.ID].Vector == nil {
		t.Errorf("reindex %+v, row %+v", resp, store.rows[ada.ID])
	}

	// Links have no direction; Get shows them
	if resp := s.Link(chronicleservice.LinkArgs{ServiceArgs: me(), FromID: engine.ID, ToID: ada.ID}); len(resp.Links) != 1 {
		t.Fatalf("link %+v", resp)
	}
	s.Link(chronicleservice.LinkArgs{ServiceArgs: me(), FromID: ada.ID, ToID: engine.ID})
	if got := s.Get(chronicleservice.GetArgs{ServiceArgs: me(), ID: ada.ID}); len(got.Links) != 1 || got.Entry.Title != "Ada" {
		t.Errorf("get %+v", got)
	}

	// Another user sees nothing
	other := me()
	other.Principal.UserID = 99
	if code := s.Get(chronicleservice.GetArgs{ServiceArgs: other, ID: ada.ID}).Common.Code; code != http.StatusNotFound {
		t.Errorf("other user: code %d", code)
	}
	if list := s.List(chronicleservice.ListArgs{ServiceArgs: other}); len(list.Entries) != 0 {
		t.Errorf("other user lists %+v", list.Entries)
	}

	// Deleting moves the children up and takes the links and the row
	child := create(t, s, chronicleservice.EntryInput{Type: "note", Title: "Notes on the engine", ParentID: engine.ID})
	if code := s.Delete(chronicleservice.DeleteArgs{ServiceArgs: me(), ID: engine.ID}).Common.Code; code != http.StatusOK {
		t.Fatalf("delete: code %d", code)
	}
	s.indexing.Wait()
	list := s.List(chronicleservice.ListArgs{ServiceArgs: me()})
	if len(list.Entries) != 2 || len(list.Links) != 0 || store.rows[engine.ID].EntryID != 0 {
		t.Errorf("after delete %+v", list)
	}
	var moved2 chronicle.Entry
	s.db.First(&moved2, child.ID)
	if moved2.ParentID != ada.ID {
		t.Errorf("child's parent %d, want %d", moved2.ParentID, ada.ID)
	}
}

func TestProposedEntriesAreIndexedOnceAccepted(t *testing.T) {
	s, store, _ := newTestService(t)
	proposed := chronicle.Entry{Tenanted: db.TenantOf(features.LocalPrincipal()), Owned: db.OwnedBy(features.LocalPrincipal()),
		Type: "note", Title: "Proposed", Fields: "{}", Status: chronicle.StatusProposed, Review: true}
	s.db.Create(&proposed)
	s.Reindex(chronicleservice.ReindexArgs{ServiceArgs: me()})
	if len(store.rows) != 0 {
		t.Fatalf("a proposed entry was indexed")
	}
	resp := s.Update(chronicleservice.UpdateArgs{ServiceArgs: me(), ID: proposed.ID, Accept: true, Reviewed: true,
		EntryInput: chronicleservice.EntryInput{Type: "note", Title: "Accepted"}})
	s.indexing.Wait()
	if resp.Entry.Status != chronicle.StatusActive || resp.Entry.Review || store.rows[proposed.ID].Title != "Accepted" {
		t.Errorf("accepted %+v, rows %+v", resp.Entry, store.rows)
	}
}
