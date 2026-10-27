package lancestore

import (
	"context"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
)

func TestStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir, 3)
	if err != nil {
		t.Fatal(err)
	}

	const bigID = uint64(1)<<62 + 1 // a TSID-sized ID survives
	err = s.Replace(ctx, "v/it's.md", []indexservice.Chunk{
		{VolumeID: bigID, Path: "v/it's.md", Ordinal: 0, Hash: "h0", Text: "x axis", Vector: []float32{1, 0, 0}},
		{VolumeID: bigID, Path: "v/it's.md", Ordinal: 1, Hash: "h1", Text: "y axis", Vector: []float32{0, 1, 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Replace(ctx, "w/b.md", []indexservice.Chunk{{VolumeID: 2, Path: "w/b.md", Hash: "h2", Text: "z", Vector: []float32{0.6, 0, 0.8}}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.VectorSearch(ctx, []float32{0.9, 0.1, 0}, 2, indexservice.Filter{})
	if err != nil || len(got) != 2 || got[0].Ordinal != 0 || got[0].VolumeID != bigID || got[0].Text != "x axis" || got[0].Hash != "h0" || got[1].Path != "w/b.md" {
		t.Fatalf("search: %+v, %v", got, err)
	}
	got, _ = s.VectorSearch(ctx, []float32{1, 0, 0}, 5, indexservice.Filter{VolumeIDs: []uint64{2}})
	if len(got) != 1 || got[0].Path != "w/b.md" {
		t.Fatalf("filtered: %+v", got)
	}

	// BM25 with stemming: "zebras" finds "zebra"
	if err := s.Replace(ctx, "w/c.md", []indexservice.Chunk{
		{VolumeID: 2, Path: "w/c.md", Ordinal: 0, Hash: "h3", Text: "zebra zebra crossing", Vector: []float32{0, 0, 1}},
		{VolumeID: 2, Path: "w/c.md", Ordinal: 1, Hash: "h4", Text: "a lone zebra", Vector: []float32{0, 0, 1}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err = s.KeywordSearch(ctx, "zebras", 5, indexservice.Filter{})
	if err != nil || len(got) != 2 || got[0].Ordinal != 0 || got[0].Path != "w/c.md" {
		t.Fatalf("keyword search: %+v, %v", got, err)
	}
	got, _ = s.KeywordSearch(ctx, "axis", 5, indexservice.Filter{VolumeIDs: []uint64{bigID}})
	if len(got) != 2 || got[0].VolumeID != bigID {
		t.Fatalf("filtered keyword search: %+v", got)
	}
	if err := s.Delete(ctx, "w/c.md"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.KeywordSearch(ctx, "zebra", 5, indexservice.Filter{}); len(got) != 0 {
		t.Fatalf("deleted chunks found: %+v", got)
	}

	vectors, err := s.Vectors(ctx, "v/it's.md")
	if err != nil || len(vectors) != 2 || vectors["h1"][1] != 1 {
		t.Fatalf("vectors: %v, %v", vectors, err)
	}

	if err := s.Replace(ctx, "v/it's.md", nil); err != nil {
		t.Fatal(err)
	}
	if paths, _ := s.Paths(ctx); len(paths) != 1 || paths[0] != "w/b.md" {
		t.Fatalf("paths %v", paths)
	}
	s.Close()

	// Reopening keeps the data; another dimension starts over
	s, err = Open(ctx, dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if paths, _ := s.Paths(ctx); len(paths) != 1 {
		t.Fatalf("reopened paths %v", paths)
	}
	s.Close()
	s, err = Open(ctx, dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if paths, _ := s.Paths(ctx); len(paths) != 0 {
		t.Fatalf("paths after a dimension change %v", paths)
	}
}
