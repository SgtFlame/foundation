package impl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// TestDocumentFiles: a document file is created empty or from content,
// refused when it is not a document or too large, and listed (forge 35a-1).
func TestDocumentFiles(t *testing.T) {
	root := t.TempDir()
	events := volumeservice.NewFileEvents()
	var seen []string
	events.Subscribe(func(e volumeservice.FileEvent) { seen = append(seen, filepath.Base(e.File)) })
	s := &service{root: root, events: events}
	args := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	path := func(f ...string) kbservice.VolumePath { return *kbservice.NewVolumePath().Folder(f...) }

	if err := os.MkdirAll(filepath.Join(root, "v"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := s.CreateVolume(volumeservice.CreateVolumeArgs{ServiceArgs: args, Path: path("v")}); r.Common.Code != 200 {
		t.Fatalf("volume: %+v", r.Common)
	}
	if r := s.CreateFile(volumeservice.CreateFileArgs{ServiceArgs: args, Path: path("v", "empty"), Type: "document"}); r.Common.Code != 200 {
		t.Fatalf("empty: %+v", r.Common)
	}
	got, _ := os.ReadFile(filepath.Join(root, "v", "empty.doc.json"))
	if !strings.Contains(string(got), `"format":1`) {
		t.Errorf("empty document %s", got)
	}
	bare := `{"type":"doc","content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Hi"}]}]}`
	if r := s.CreateFile(volumeservice.CreateFileArgs{ServiceArgs: args, Path: path("v", "hi"), Type: "document", Content: bare}); r.Common.Code != 200 {
		t.Fatalf("hi: %+v", r.Common)
	}
	got, _ = os.ReadFile(filepath.Join(root, "v", "hi.doc.json"))
	if text, err := volumeservice.TextOf("document", got); err != nil || text != "# Hi\n" {
		t.Errorf("text %q %v", text, err)
	}
	if r := s.CreateFile(volumeservice.CreateFileArgs{ServiceArgs: args, Path: path("v", "bad"), Type: "document", Content: "# not json"}); r.Common.Code != 400 || r.Common.ErrorCode != "invalid_document" {
		t.Errorf("bad: %+v", r.Common)
	}
	big := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + strings.Repeat("x", volumeservice.MaxDocumentBytes) + `"}]}]}`
	if r := s.CreateFile(volumeservice.CreateFileArgs{ServiceArgs: args, Path: path("v", "big"), Type: "document", Content: big}); r.Common.Code != 413 || r.Common.ErrorCode != "document_too_large" {
		t.Errorf("big: %+v", r.Common)
	}
	list := s.ListFolder(volumeservice.ListFolderArgs{ServiceArgs: args, Path: path("v")})
	var names []string
	for _, f := range list.Files {
		names = append(names, f.Name+":"+f.Type)
	}
	if strings.Join(names, ",") != "empty:document,hi:document" {
		t.Errorf("listed %v", names)
	}
	if strings.Join(seen, ",") != "empty.doc.json,hi.doc.json" {
		t.Errorf("events %v", seen)
	}
}
