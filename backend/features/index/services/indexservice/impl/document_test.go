package impl

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// TestIndexDocument: a document file is chunked by its derived Markdown,
// found by Find, and its chunks' lines are lines of that Markdown (forge
// 35a-1).
func TestIndexDocument(t *testing.T) {
	f := newFixture(t)
	doc := `{"format":1,"doc":{"type":"doc","content":[` +
		`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Apples"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"apple pie and apple tart. ` + filler + `"}]},` +
		`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Cherries"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"cherry jam and cherry cola. ` + filler + `"}]}]}}`
	file := f.write(t, "vol/fruit.doc.json", doc)
	if !indexable(file) {
		t.Fatal("a document is not indexable")
	}
	resp := f.svc.IndexFile(indexservice.IndexFileArgs{Path: *kbservice.NewVolumePath().Folder("vol", "fruit"), Type: "document"})
	if resp.Common.Code != http.StatusOK || resp.Chunks != 2 {
		t.Fatalf("IndexFile: %+v", resp)
	}
	meta, err := indexservice.ReadMeta(file)
	if err != nil || meta.Chunks[1].Heading != "Cherries" || meta.Chunks[1].StartLine != 5 {
		t.Fatalf("meta %+v %v", meta, err)
	}
	if !strings.HasSuffix(indexservice.MetaFile(file), ".fruit.doc.json.index.json") {
		t.Errorf("sidecar %s", indexservice.MetaFile(file))
	}
	hits := f.svc.Find(indexservice.FindArgs{Query: "cherry", Weight: 0.5, Count: 5}).Hits
	if len(hits) != 1 || strings.Join(hits[0].Folders, "/") != "vol/fruit" || hits[0].Chunks[0].Heading != "Cherries" {
		t.Fatalf("hits %+v", hits)
	}
	if strings.Contains(f.store.chunks["vol/fruit.doc.json"][0].Text, `"type"`) {
		t.Error("a chunk holds the document's JSON")
	}

	// A file that is not a document is refused
	f.write(t, "vol/broken.doc.json", "# not json")
	if resp := f.svc.IndexFile(indexservice.IndexFileArgs{Path: *kbservice.NewVolumePath().Folder("vol", "broken"), Type: "document"}); resp.Common.Code != http.StatusUnprocessableEntity {
		t.Errorf("broken: %+v", resp.Common)
	}
	_ = os.Remove(filepath.Join(f.root, "vol", "broken.doc.json"))
}
