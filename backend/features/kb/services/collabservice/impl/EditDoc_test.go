package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

// memDoc is a document held in memory, read-only for user 2.
type memDoc struct {
	mu   sync.Mutex
	text string
}

func (m *memDoc) Key() string  { return "mem" }
func (m *memDoc) Name() string { return "the memo" }
func (m *memDoc) Load(features.ServiceArgs) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.text, nil
}
func (m *memDoc) Save(_ features.ServiceArgs, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.text = text
	return nil
}
func (m *memDoc) Access(args features.ServiceArgs) (bool, *features.ServiceResponse) {
	return args.Principal.UserID == 2, nil
}

func TestEditDoc(t *testing.T) {
	doc := &memDoc{text: "Bookings: a table holds four. Deposits: none."}
	kinds := func(kind string) (collabservice.DocumentKind, bool) {
		return collabservice.DocumentKind{Open: func(features.ServiceArgs, json.RawMessage) (collabservice.Document, *features.ServiceResponse) {
			return doc, nil
		}}, kind == "mem"
	}
	s := newService(WithGracePeriod(time.Hour), WithDocumentKinds(kinds))
	as := func(user uint64) features.ServiceArgs {
		return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}
	}
	at := func(n int) *int { return &n }
	edit := func(user uint64, each bool, edits ...collabservice.Edit) collabservice.EditDocResponse {
		return s.EditDoc(collabservice.EditDocArgs{ServiceArgs: as(user), Kind: "mem", Ref: json.RawMessage(`{}`), Edits: edits, Each: each})
	}

	// Nobody has it open: it is loaded, changed and saved
	if resp := edit(1, false, collabservice.Edit{Start: at(24), End: at(28), Find: "four", Replace: "six"}); resp.Common.Code != 200 || resp.Live || !resp.Applied[0] {
		t.Fatalf("EditDoc = %+v", resp)
	}
	if text, _ := doc.Load(as(1)); text != "Bookings: a table holds six. Deposits: none." {
		t.Fatalf("doc holds %q", text)
	}
	// One that cannot apply fails them all, unless each is asked
	if resp := edit(1, false, collabservice.Edit{Find: "six", Replace: "eight"}, collabservice.Edit{Find: "lunch"}); resp.Common.Code != http.StatusConflict {
		t.Fatalf("a missing find gave %+v", resp)
	}
	if text, _ := doc.Load(as(1)); text != "Bookings: a table holds six. Deposits: none." {
		t.Fatalf("a failed batch changed it: %q", text)
	}
	resp := edit(1, true, collabservice.Edit{Find: "six", Replace: "eight"}, collabservice.Edit{Find: "lunch"})
	if resp.Common.Code != 200 || !resp.Applied[0] || resp.Applied[1] || resp.Problems[1] == "" {
		t.Fatalf("each = %+v", resp)
	}
	// A reader cannot; an unknown kind is refused
	if resp := edit(2, false, collabservice.Edit{Find: "eight", Replace: "ten"}); resp.Common.Code != http.StatusForbidden {
		t.Fatalf("a reader's edit gave %+v", resp)
	}
	if resp := s.EditDoc(collabservice.EditDocArgs{ServiceArgs: as(1), Kind: "other", Edits: []collabservice.Edit{{Find: "x"}}}); resp.Common.Code != http.StatusBadRequest {
		t.Fatalf("an unknown kind gave %+v", resp)
	}

	// Open: the writer is sent the range as a unique find/replace
	id := s.OpenDoc(collabservice.OpenDocArgs{ServiceArgs: as(1), Kind: "mem", Ref: json.RawMessage(`{}`)}).FileID
	conn := newFakeConn()
	go s.Collaborate(collabservice.CollaborateArgs{ServiceArgs: as(1), FileID: id, Conn: conn})
	conn.expectControl(t, "writer role", func(c control) bool { return c.Op == "role" && c.Writer })
	conn.in <- controlMessage(control{Op: "seeded"})
	conn.expectControl(t, "ready", func(c control) bool { return c.Op == "ready" })
	answered := make(chan collabservice.EditDocResponse, 1)
	go func() { answered <- edit(1, false, collabservice.Edit{Start: at(41), End: at(45), Find: "none", Replace: "£10 a head"}) }()
	var asked control
	conn.expectControl(t, "edit", func(c control) bool { asked = c; return c.Op == "edit" })
	if len(asked.Edits) != 1 || asked.Edits[0].Find != "none" || asked.Edits[0].Replace != "£10 a head" || asked.Edits[0].Start != nil {
		t.Fatalf("edit sent = %+v", asked)
	}
	conn.in <- controlMessage(control{Op: "edited", ID: asked.ID, Code: 200})
	if resp := <-answered; resp.Common.Code != 200 || !resp.Live {
		t.Fatalf("live EditDoc = %+v", resp)
	}
}
