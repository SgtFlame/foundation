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

func TestEditFile(t *testing.T) {
	root := t.TempDir()
	volume := filepath.Join(root, "vol")
	file := filepath.Join(volume, "note.md")
	if err := os.MkdirAll(volume, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(volume, volumeservice.MarkerFile), []byte("{}"), 0o644)
	_ = os.WriteFile(file, []byte("one two three"), 0o644)
	s := newService(WithRoot(root), WithGracePeriod(time.Hour), WithFileEvents(volumeservice.NewFileEvents()))
	sa := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	edit := func(edits ...collabservice.Edit) collabservice.EditFileResponse {
		return s.EditFile(collabservice.EditFileArgs{ServiceArgs: sa, Path: *kbservice.NewVolumePath().Folder("vol", "note"), Type: "markdown", Edits: edits})
	}

	// Nobody has it open: the file changes
	if resp := edit(collabservice.Edit{Find: "two", Replace: "2"}); resp.Common.Code != 200 || resp.Live {
		t.Fatalf("EditFile = %+v", resp)
	}
	if content, _ := os.ReadFile(file); string(content) != "one 2 three" {
		t.Fatalf("file holds %q", content)
	}
	if resp := edit(collabservice.Edit{Find: "four"}); resp.Common.Code != 409 {
		t.Fatalf("a missing find gave %+v", resp)
	}

	// Open, the writer applies it
	id := s.OpenFile(collabservice.OpenFileArgs{ServiceArgs: sa, Path: *kbservice.NewVolumePath().Folder("vol", "note"), Type: "markdown"}).FileID
	conn := newFakeConn()
	go s.Collaborate(collabservice.CollaborateArgs{ServiceArgs: sa, FileID: id, Conn: conn})
	conn.expectControl(t, "writer role", func(c control) bool { return c.Op == "role" && c.Writer })
	conn.in <- controlMessage(control{Op: "seeded"})
	conn.expectControl(t, "ready", func(c control) bool { return c.Op == "ready" })
	answered := make(chan collabservice.EditFileResponse, 1)
	go func() { answered <- edit(collabservice.Edit{Find: "three", Replace: "3"}) }()
	var asked control
	conn.expectControl(t, "edit", func(c control) bool { asked = c; return c.Op == "edit" })
	if len(asked.Edits) != 1 || asked.Edits[0].Find != "three" {
		t.Fatalf("edit sent = %+v", asked)
	}
	conn.in <- controlMessage(control{Op: "edited", ID: asked.ID, Code: 200})
	if resp := <-answered; resp.Common.Code != 200 || !resp.Live {
		t.Fatalf("live EditFile = %+v", resp)
	}
}
