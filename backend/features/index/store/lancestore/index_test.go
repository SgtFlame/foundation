package lancestore

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	indeximpl "github.com/SgtFlame/foundation/backend/features/index/services/indexservice/impl"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// topicEmbedder embeds a text by which of a few topics it mentions.
type topicEmbedder struct{}

var topics = []string{"rocket", "garden", "piano"}

func (topicEmbedder) Embed(args embedservice.EmbedArgs) embedservice.EmbedResponse {
	resp := embedservice.EmbedResponse{Common: features.ServiceResponse{Code: http.StatusOK}, Model: "topics", Dimensions: len(topics)}
	for _, text := range args.Texts {
		v := make([]float32, len(topics))
		var norm float32
		for i, t := range topics {
			v[i] = float32(strings.Count(strings.ToLower(text), t)) + 0.01
			norm += v[i] * v[i]
		}
		for i := range v {
			v[i] /= norm
		}
		resp.Vectors = append(resp.Vectors, v)
	}
	return resp
}

func (topicEmbedder) Describe(embedservice.DescribeArgs) embedservice.DescribeResponse {
	return embedservice.DescribeResponse{Common: features.ServiceResponse{Code: http.StatusOK}, Model: "topics", Dimensions: len(topics), MaxBatch: 100}
}

// TestIndexOnLanceDB runs the index service end to end on a real store.
func TestIndexOnLanceDB(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	vol := filepath.Join(root, "vol")
	if err := os.MkdirAll(vol, 0o755); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(volumeservice.Marker{ID: 1 << 60})
	os.WriteFile(filepath.Join(vol, volumeservice.MarkerFile), marker, 0o644)
	os.WriteFile(filepath.Join(vol, "space.md"), []byte("# Space\n\nThe rocket launched at dawn; rocket engines roared.\n"), 0o644)
	os.WriteFile(filepath.Join(vol, "home.md"), []byte("# Home\n\nThe garden needs watering. A piano sits by the garden window.\n"), 0o644)

	store, err := Open(ctx, filepath.Join(t.TempDir(), "lancedb"), len(topics))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := indeximpl.CreateService(indeximpl.WithRoot(root), indeximpl.WithEmbedder(topicEmbedder{}), indeximpl.WithStore(store), indeximpl.WithDebounce(10*time.Millisecond))

	for _, name := range []string{"space", "home"} {
		resp := svc.IndexFile(indexservice.IndexFileArgs{Path: *kbservice.NewVolumePath().Folder("vol", name), Type: "markdown"})
		if resp.Common.Code != http.StatusOK || resp.Chunks != 1 {
			t.Fatalf("IndexFile %s: %+v", name, resp)
		}
	}

	find := func(query string, weight float64) []indexservice.Hit {
		t.Helper()
		resp := svc.Find(indexservice.FindArgs{Query: query, Weight: weight, Count: 5})
		if resp.Common.Code != http.StatusOK {
			t.Fatalf("Find %q: %d %s", query, resp.Common.Code, resp.Common.Message)
		}
		return resp.Hits
	}
	// Vector search alone
	if hits := find("rocket", 1); len(hits) == 0 || hits[0].Folders[1] != "space" || hits[0].VolumeID != 1<<60 {
		t.Fatalf("vector hits %+v", hits)
	}
	// Keyword, with stemming: "watered" finds "watering"
	hits := find("watered", 0)
	if len(hits) != 1 || hits[0].Folders[1] != "home" || hits[0].Chunks[0].KeywordRank != 1 || hits[0].Chunks[0].StartLine != 1 {
		t.Fatalf("keyword hits %+v", hits)
	}
	if hits := find("piano rocket", 0.5); len(hits) != 2 {
		t.Fatalf("hybrid hits %+v", hits)
	}
}
