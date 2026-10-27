package entrystore

import (
	"context"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/chronicle"
)

func TestStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	const bigID = uint64(1)<<62 + 1 // a TSID-sized ID survives
	me, other := chronicle.Owner{TenantID: 1, UserID: 1}, chronicle.Owner{TenantID: 1, UserID: 2}
	rows := []chronicle.Row{
		{EntryID: bigID, TenantID: 1, OwnerID: 1, Hash: "h1", Title: "Ada", Text: "Ada Lovelace wrote programs for the analytical engine", Vector: []float32{1, 0, 0}},
		{EntryID: 2, TenantID: 1, OwnerID: 1, Hash: "h2", Title: "Engines", Text: "steam engines and zebras", Vector: []float32{0, 1, 0}},
		{EntryID: 3, TenantID: 1, OwnerID: 1, Hash: "h3", Title: "Unembedded", Text: "zebra crossing notes"}, // no key: keywords only
		{EntryID: 4, TenantID: 1, OwnerID: 2, Hash: "h4", Title: "Not mine", Text: "zebra", Vector: []float32{1, 0, 0}},
		{EntryID: 5, TenantID: 1, OwnerID: 2, Shared: true, Hash: "h5", Title: "Shared with the tenant", Text: "okapi", Vector: []float32{0, 0, 1}},
		{EntryID: 6, TenantID: 2, OwnerID: 1, Shared: true, Hash: "h6", Title: "Another tenant's", Text: "okapi"},
	}
	for _, r := range rows {
		if err := s.Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.VectorSearch(ctx, me, []float32{0.9, 0.1, 0}, 5)
	if err != nil || len(got) != 3 || got[0].EntryID != bigID || got[0].Title != "Ada" || got[0].Hash != "h1" || got[1].EntryID != 2 || got[2].EntryID != 5 {
		t.Fatalf("vector search: %+v, %v", got, err)
	}
	// BM25 with stemming finds the unembedded entry too, and never another user's
	got, err = s.KeywordSearch(ctx, me, "zebras", 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("keyword search: %+v, %v", got, err)
	}
	if got, _ := s.KeywordSearch(ctx, other, "zebra", 5); len(got) != 1 || got[0].EntryID != 4 {
		t.Fatalf("other's keyword search: %+v", got)
	}

	// A shared entry is found by the tenant's other members, never another tenant's
	if got, _ := s.KeywordSearch(ctx, me, "okapi", 5); len(got) != 1 || got[0].EntryID != 5 {
		t.Fatalf("shared keyword search: %+v", got)
	}

	// OthersOnly leaves out the user's own shared rows
	if got, _ := s.KeywordSearch(ctx, chronicle.Owner{TenantID: 1, UserID: 2, OthersOnly: true}, "okapi", 5); len(got) != 0 {
		t.Fatalf("others-only search found the user's own: %+v", got)
	}

	// Put replaces; Delete removes
	if err := s.Put(ctx, chronicle.Row{EntryID: 2, TenantID: 1, OwnerID: 1, Hash: "h2b", Title: "Engines", Text: "difference engines", Vector: []float32{0, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.KeywordSearch(ctx, me, "zebra", 5); len(got) != 0 {
		t.Fatalf("replaced or deleted entries found: %+v", got)
	}
	hashes, err := s.Hashes(ctx)
	if err != nil || len(hashes) != 4 || hashes[2] != "h2b" || hashes[bigID] != "h1" {
		t.Fatalf("hashes %v, %v", hashes, err)
	}
	s.Close()

	// Another dimension starts over
	s, err = Open(ctx, dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if hashes, _ := s.Hashes(ctx); len(hashes) != 0 {
		t.Fatalf("hashes after a dimension change %v", hashes)
	}
}
