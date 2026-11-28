package impl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// TestDocumentRoom: a document room seeds from the file, an unchanged save
// keeps it byte for byte, a bare doc is wrapped, and a save that is not a
// document is refused with its code (forge 35a-1).
func TestDocumentRoom(t *testing.T) {
	root := t.TempDir()
	volume := filepath.Join(root, "vol")
	file := filepath.Join(volume, "page.doc.json")
	_ = os.MkdirAll(volume, 0o755)
	_ = os.WriteFile(filepath.Join(volume, volumeservice.MarkerFile), []byte("{}"), 0o644)
	original := `{"format": 1, "doc": {"type": "doc", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Hi"}]}]}}`
	_ = os.WriteFile(file, []byte(original), 0o644)

	s := newService(WithRoot(root), WithGracePeriod(time.Hour), WithFileEvents(volumeservice.NewFileEvents()))
	path := kbservice.NewVolumePath().Folder("vol", "page")
	resp := s.OpenFile(collabservice.OpenFileArgs{Path: *path, Type: "document"})
	if resp.Common.Code != 200 {
		t.Fatalf("OpenFile: %+v", resp.Common)
	}
	a := newFakeConn()
	args := features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{TenantID: features.LocalTenantID}}
	go s.Collaborate(collabservice.CollaborateArgs{ServiceArgs: args, FileID: resp.FileID, Conn: a})
	a.expectControl(t, "writer role with the file as seed", func(c control) bool {
		return c.Op == "role" && c.Writer && c.Seed != nil && *c.Seed == original
	})
	a.in <- syncMessage(syncUpdate, []byte{1, 2, 3})
	a.in <- controlMessage(control{Op: "seeded"})
	a.expectControl(t, "ready", func(c control) bool { return c.Op == "ready" })

	a.in <- controlMessage(control{Op: "save", Markdown: original})
	a.expectControl(t, "saved", func(c control) bool { return c.Op == "saved" && c.Code == 200 })
	if got, _ := os.ReadFile(file); string(got) != original {
		t.Fatalf("an unchanged save changed the file: %s", got)
	}

	a.in <- controlMessage(control{Op: "save", Markdown: `{"type":"doc","content":[]}`})
	a.expectControl(t, "saved bare", func(c control) bool { return c.Op == "saved" && c.Code == 200 })
	if got, _ := os.ReadFile(file); string(got) != `{"format":1,"doc":{"type":"doc","content":[]}}` {
		t.Fatalf("a bare doc was stored as %s", got)
	}

	a.in <- controlMessage(control{Op: "save", Markdown: "# markdown"})
	a.expectControl(t, "refused", func(c control) bool {
		return c.Op == "saved" && c.Code == 400 && c.ErrorCode == "invalid_document"
	})
	if got, _ := os.ReadFile(file); string(got) != `{"format":1,"doc":{"type":"doc","content":[]}}` {
		t.Fatalf("a refused save changed the file: %s", got)
	}
}
