package impl

import (
	"net/http"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// people's workspaces for the chats-per-workspace tests (forge 16d), by
// user id; Default is made on first read, so it may be missing.
type people map[uint64][]string

func (p people) wire(s *service) {
	s.workspaceList = func(args features.ServiceArgs) ([]string, error) {
		return slices.Clone(p[args.Principal.UserID]), nil
	}
	s.workspaces = func(args features.ServiceArgs, id string) (conversation.WorkspaceKnowledge, bool) {
		if slices.Contains(p[args.Principal.UserID], id) {
			return conversation.WorkspaceKnowledge{ID: id}, true
		}
		return conversation.WorkspaceKnowledge{}, false
	}
}

func listed(t *testing.T, s *service, args features.ServiceArgs, ws string) ([]uint64, map[uint64]conversationservice.Places) {
	t.Helper()
	resp := s.List(conversationservice.ListArgs{ServiceArgs: args, Workspace: ws, Limit: 50})
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("list %q: %d %s", ws, resp.Common.Code, resp.Common.Message)
	}
	var ids []uint64
	for _, c := range resp.Conversations {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	return ids, resp.Places
}

func started(t *testing.T, s *service, args features.ServiceArgs, ws string, conversationID uint64) uint64 {
	t.Helper()
	ev := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "hello from " + ws, Workspace: ws, ConversationID: conversationID}))
	if len(ev[conversationservice.EventStarted]) != 1 {
		t.Fatalf("send in %q: %+v", ws, ev[conversationservice.EventFailed])
	}
	return ev[conversationservice.EventStarted][0].Conversation.ID
}

func sorted(ids ...uint64) []uint64 {
	slices.Sort(ids)
	return ids
}

// forge 16d: a conversation belongs to the workspace it was started in,
// is listed there and in the workspaces it is shown in, and only its
// owner moves or shows it, in their own workspaces.
func TestChatsBelongToTheirWorkspace(t *testing.T) {
	s, _ := newTestService(t, &fakeChat{})
	ana := features.ServiceArgs{Principal: features.LocalPrincipal()}
	bo := ana
	bo.Principal.UserID = 99
	ws := people{ana.Principal.UserID: {"default", "support", "hr"}, 99: {"default", "bos"}}
	ws.wire(s)

	sup := started(t, s, ana, "support", 0)
	def := started(t, s, ana, "", 0) // no workspace named: Default
	var stored conversation.Conversation
	s.db.First(&stored, sup)
	if stored.WorkspaceID != "support" {
		t.Errorf("stored workspace %q", stored.WorkspaceID)
	}
	// Continuing it from another workspace's tab keeps it where it belongs
	started(t, s, ana, "hr", sup)
	if got, places := listed(t, s, ana, "support"); !slices.Equal(got, []uint64{sup}) || places[sup].Own != "support" {
		t.Errorf("support lists %v %+v", got, places)
	}
	if got, _ := listed(t, s, ana, "default"); !slices.Equal(got, []uint64{def}) {
		t.Errorf("default lists %v", got)
	}
	if got, _ := listed(t, s, ana, ""); !slices.Equal(got, []uint64{def}) {
		t.Errorf("no workspace lists %v, want Default's", got)
	}
	if got, _ := listed(t, s, ana, "hr"); len(got) != 0 {
		t.Errorf("hr lists %v", got)
	}
	// The Dashboard's top conversations by id ignore workspaces
	if resp := s.List(conversationservice.ListArgs{ServiceArgs: ana, IDs: []uint64{sup, def}}); len(resp.Conversations) != 2 {
		t.Errorf("by ids: %d", len(resp.Conversations))
	}

	// Someone else's workspace, or one that does not exist, is refused
	for _, c := range []struct {
		who features.ServiceArgs
		ws  string
	}{{bo, "support"}, {ana, "bos"}, {ana, "nope"}} {
		if resp := s.List(conversationservice.ListArgs{ServiceArgs: c.who, Workspace: c.ws}); resp.Common.Code != http.StatusNotFound || resp.Common.ErrorCode != "workspace_not_found" {
			t.Errorf("list %q by %d: %d %s", c.ws, c.who.Principal.UserID, resp.Common.Code, resp.Common.ErrorCode)
		}
	}
	if got, _ := listed(t, s, bo, "default"); len(got) != 0 {
		t.Errorf("Bo sees %v", got)
	}

	// Show in… Default: listed in both
	shown := []string{"default"}
	resp := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, ShownIn: &shown})
	if resp.Common.Code != http.StatusOK || resp.Places.Own != "support" || !slices.Equal(resp.Places.ShownIn, shown) {
		t.Fatalf("show in: %+v", resp)
	}
	// again changes nothing
	if again := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, ShownIn: &shown}); !slices.Equal(again.Places.ShownIn, shown) {
		t.Errorf("show in again: %+v", again.Places)
	}
	if got, places := listed(t, s, ana, "default"); !slices.Equal(got, sorted(def, sup)) || !slices.Equal(places[sup].ShownIn, shown) {
		t.Errorf("default lists %v %+v", got, places)
	}
	if got, _ := listed(t, s, ana, "support"); !slices.Equal(got, []uint64{sup}) {
		t.Errorf("support lists %v", got)
	}

	// Only the owner, in their own workspaces
	bosShown := []string{"bos"}
	if r := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: bo, ConversationID: sup, ShownIn: &bosShown}); r.Common.Code != http.StatusNotFound {
		t.Errorf("Bo shows Ana's: %d", r.Common.Code)
	}
	if r := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: bo, ConversationID: sup, Move: "bos"}); r.Common.Code != http.StatusNotFound {
		t.Errorf("Bo moves Ana's: %d", r.Common.Code)
	}
	if r := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, ShownIn: &bosShown}); r.Common.ErrorCode != "workspace_not_found" {
		t.Errorf("Ana shows hers in Bo's: %+v", r.Common)
	}
	if r := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, Move: "bos"}); r.Common.ErrorCode != "workspace_not_found" {
		t.Errorf("Ana moves hers to Bo's: %+v", r.Common)
	}

	// Move to… HR: it leaves Support, and stays shown in Default
	if r := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, Move: "hr"}); r.Places.Own != "hr" || !slices.Equal(r.Places.ShownIn, shown) {
		t.Errorf("move: %+v", r.Places)
	}
	if got, _ := listed(t, s, ana, "support"); len(got) != 0 {
		t.Errorf("support lists %v after the move", got)
	}
	if got, _ := listed(t, s, ana, "hr"); !slices.Equal(got, []uint64{sup}) {
		t.Errorf("hr lists %v", got)
	}
	// Moved to a workspace it was shown in: no longer shown there besides
	if r := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, Move: "default"}); r.Places.Own != "default" || len(r.Places.ShownIn) != 0 {
		t.Errorf("move to default: %+v", r.Places)
	}
	var rows int64
	s.db.Model(&conversation.ConversationWorkspace{}).Where("conversation_id = ?", sup).Count(&rows)
	if rows != 0 {
		t.Errorf("%d rows left", rows)
	}
	// Show in… nothing clears the rows
	both := []string{"support", "hr", "default"}
	s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, ShownIn: &both})
	none := []string{}
	if r := s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: sup, ShownIn: &none}); len(r.Places.ShownIn) != 0 {
		t.Errorf("cleared: %+v", r.Places)
	}
}

// forge 16d: a deleted workspace's conversations are Default's at once,
// and Default's list writes that down once they are old enough; the
// rows showing others there go.
func TestDeletedWorkspacesChatsMoveToDefault(t *testing.T) {
	s, _ := newTestService(t, &fakeChat{})
	ana := features.ServiceArgs{Principal: features.LocalPrincipal()}
	ws := people{ana.Principal.UserID: {"default", "support", "hr"}}
	ws.wire(s)

	sup := started(t, s, ana, "support", 0)
	hr := started(t, s, ana, "hr", 0)
	shown := []string{"support"}
	s.SetWorkspaces(conversationservice.SetWorkspacesArgs{ServiceArgs: ana, ConversationID: hr, ShownIn: &shown})

	ws[ana.Principal.UserID] = []string{"default", "hr"} // Support is deleted
	if got, places := listed(t, s, ana, "default"); !slices.Equal(got, []uint64{sup}) || places[sup].Own != "default" {
		t.Errorf("default lists %v %+v", got, places)
	}
	if got, places := listed(t, s, ana, "hr"); !slices.Equal(got, []uint64{hr}) || len(places[hr].ShownIn) != 0 {
		t.Errorf("hr lists %v %+v", got, places)
	}
	// Too new to write down yet (another process may not see a new workspace)
	var c conversation.Conversation
	s.db.First(&c, sup)
	if c.WorkspaceID != "support" {
		t.Errorf("written down at once: %q", c.WorkspaceID)
	}
	old := time.Now().Add(-2 * settleAfter)
	s.db.Model(&conversation.Conversation{}).Where("1 = 1").UpdateColumn("created_at", old)
	s.db.Model(&conversation.ConversationWorkspace{}).Where("1 = 1").UpdateColumn("created_at", old)
	listed(t, s, ana, "default")
	s.db.First(&c, sup)
	var rows int64
	s.db.Model(&conversation.ConversationWorkspace{}).Count(&rows)
	if c.WorkspaceID != "default" || rows != 0 {
		t.Errorf("after settling: workspace %q, %d rows", c.WorkspaceID, rows)
	}
	var h conversation.Conversation
	s.db.First(&h, hr)
	if h.WorkspaceID != "hr" {
		t.Errorf("hr's conversation moved: %q", h.WorkspaceID)
	}
}

// forge 16d: conversations from before workspaces (a table without the
// column) are Default's after the migration.
func TestOldConversationsAreDefaults(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "old.db")))
	if err != nil {
		t.Fatal(err)
	}
	type oldConversation struct {
		db.Model
		db.Tenanted
		db.Owned
		Title       string
		LastRoundAt time.Time
	}
	old := gdb.Table("conversations")
	if err := old.AutoMigrate(&oldConversation{}); err != nil {
		t.Fatal(err)
	}
	p := features.LocalPrincipal()
	row := oldConversation{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), Title: "Before"}
	if err := gdb.Table("conversations").Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, conversation.Models()...); err != nil {
		t.Fatal(err)
	}
	s := newService(WithDB(gdb))
	people{p.UserID: {"default", "support"}}.wire(s)
	args := features.ServiceArgs{Principal: p}
	if got, places := listed(t, s, args, "default"); !slices.Equal(got, []uint64{row.ID}) || places[row.ID].Own != "default" {
		t.Errorf("default lists %v %+v", got, places)
	}
	if got, _ := listed(t, s, args, "support"); len(got) != 0 {
		t.Errorf("support lists %v", got)
	}
	gdb.Model(&conversation.Conversation{}).Where("1 = 1").UpdateColumn("created_at", time.Now().Add(-2*settleAfter))
	listed(t, s, args, "default")
	var c conversation.Conversation
	gdb.First(&c, row.ID)
	if c.WorkspaceID != "default" {
		t.Errorf("written down as %q", c.WorkspaceID)
	}
}
