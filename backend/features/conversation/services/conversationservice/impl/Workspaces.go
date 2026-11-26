package impl

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Chats per workspace (forge 16d). A conversation belongs to one of its
// owner's workspaces (Conversation.WorkspaceID) and may be shown in others
// (ConversationWorkspace rows). Whether a workspace still exists is read
// from the workspace feature on every call, never kept: a conversation
// whose workspace is gone is Default's at once (conversation.OwnWorkspace),
// in every process, without waiting for anything to move it.

// settleAfter is how old a conversation or row must be before Default's
// list writes down that its workspace is gone. A workspace made in another
// process in the same instant may not be in this process's view yet; its
// conversations are listed in Default meanwhile, but not moved.
const settleAfter = time.Minute

// workspaceIDs lists the caller's workspaces, Default always among them;
// false when the service has no workspace list.
func (s *service) workspaceIDs(args features.ServiceArgs) ([]string, bool, *features.ServiceResponse) {
	if s.workspaceList == nil {
		return nil, false, nil
	}
	ids, err := s.workspaceList(args)
	if err != nil {
		return nil, true, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if !slices.Contains(ids, conversation.DefaultWorkspace) {
		ids = append(ids, conversation.DefaultWorkspace)
	}
	return ids, true, nil
}

// knownWorkspace refuses an id that is not one of the caller's workspaces.
func knownWorkspace(args features.ServiceArgs, ids []string, id string) *features.ServiceResponse {
	if slices.Contains(ids, id) {
		return nil
	}
	return args.Fail(http.StatusNotFound, log.Code("workspace_not_found").With("workspace", id).New("you have no such workspace"))
}

// others are the workspaces besides Default.
func others(ids []string) []string {
	return slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == conversation.DefaultWorkspace })
}

// inWorkspace narrows q to the conversations listed in workspace ws: its
// own (Default's are those whose workspace is not another that exists)
// and those shown there.
func (s *service) inWorkspace(args features.ServiceArgs, q *gorm.DB, ws string, ids []string) *gorm.DB {
	shown := s.db.Model(&conversation.ConversationWorkspace{}).Select("conversation_id").
		Where("tenant_id = ? AND owner_id = ? AND workspace_id = ?", args.Principal.TenantID, args.Principal.UserID, ws)
	if ws != conversation.DefaultWorkspace {
		return q.Where(s.db.Where("workspace_id = ?", ws).Or("id IN (?)", shown))
	}
	if rest := others(ids); len(rest) > 0 {
		return q.Where(s.db.Where("workspace_id NOT IN ?", rest).Or("id IN (?)", shown))
	}
	return q // every conversation is Default's
}

// settle writes down what Default's list reads (forge 16d): conversations
// whose workspace is gone (or from before workspaces) now belong to
// Default, and rows showing a conversation in a workspace that is gone are
// deleted. It is idempotent, so any process may run it at any time.
func (s *service) settle(args features.ServiceArgs, ids []string) {
	cutoff := time.Now().Add(-settleAfter)
	p := args.Principal
	q := s.db.WithContext(args.Context()).Model(&conversation.Conversation{}).
		Where("tenant_id = ? AND owner_id = ? AND workspace_id <> ? AND created_at < ?", p.TenantID, p.UserID, conversation.DefaultWorkspace, cutoff)
	if rest := others(ids); len(rest) > 0 {
		q = q.Where("workspace_id NOT IN ?", rest)
	}
	if err := q.UpdateColumn("workspace_id", conversation.DefaultWorkspace).Error; err != nil {
		log.With("userId", p.UserID).Warn(args.Context(), "cannot move conversations to Default: %v", err)
	}
	err := s.db.WithContext(args.Context()).Where("tenant_id = ? AND owner_id = ? AND workspace_id NOT IN ? AND created_at < ?", p.TenantID, p.UserID, ids, cutoff).
		Delete(&conversation.ConversationWorkspace{}).Error
	if err != nil {
		log.With("userId", p.UserID).Warn(args.Context(), "cannot delete the rows of deleted workspaces: %v", err)
	}
}

// placesOf reads where each conversation is listed.
func (s *service) placesOf(args features.ServiceArgs, list []conversation.Conversation, ids []string) (map[uint64]conversationservice.Places, error) {
	out := make(map[uint64]conversationservice.Places, len(list))
	if len(list) == 0 {
		return out, nil
	}
	cids := make([]uint64, len(list))
	for i, c := range list {
		cids[i] = c.ID
		out[c.ID] = conversationservice.Places{Own: conversation.OwnWorkspace(c, ids)}
	}
	var rows []conversation.ConversationWorkspace
	if err := s.owned(args).Where("conversation_id IN ?", cids).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		p := out[r.ConversationID]
		if r.WorkspaceID != p.Own && slices.Contains(ids, r.WorkspaceID) && !slices.Contains(p.ShownIn, r.WorkspaceID) {
			p.ShownIn = append(p.ShownIn, r.WorkspaceID)
			out[r.ConversationID] = p
		}
	}
	return out, nil
}

// SetWorkspaces moves one of the caller's conversations to another of
// their workspaces, or sets the others it is shown in (forge 16d). Both
// are idempotent: the same call again changes nothing.
func (s *service) SetWorkspaces(args conversationservice.SetWorkspacesArgs) conversationservice.SetWorkspacesResponse {
	fail := func(f *features.ServiceResponse) conversationservice.SetWorkspacesResponse {
		return conversationservice.SetWorkspacesResponse{Common: *f}
	}
	if failed := check(args.ServiceArgs, "conversation.write", args.ConversationID); failed != nil {
		return fail(failed)
	}
	c, failed := s.find(args.ServiceArgs, args.ConversationID)
	if failed != nil {
		return fail(failed)
	}
	ids, wired, failed := s.workspaceIDs(args.ServiceArgs)
	switch {
	case failed != nil:
		return fail(failed)
	case !wired:
		return fail(args.Fail(http.StatusServiceUnavailable, log.Code("no_workspaces").New("this server has no workspaces")))
	}
	move := strings.TrimSpace(args.Move)
	if move != "" {
		if failed := knownWorkspace(args.ServiceArgs, ids, move); failed != nil {
			return fail(failed)
		}
	}
	var shown []string
	if args.ShownIn != nil {
		for _, id := range *args.ShownIn {
			id = strings.TrimSpace(id)
			if failed := knownWorkspace(args.ServiceArgs, ids, id); failed != nil {
				return fail(failed)
			}
			if !slices.Contains(shown, id) {
				shown = append(shown, id)
			}
		}
	}
	own := conversation.OwnWorkspace(c, ids)
	if move != "" {
		own = move
	}
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		p := args.Principal
		rows := tx.Where("tenant_id = ? AND owner_id = ? AND conversation_id = ?", p.TenantID, p.UserID, c.ID)
		if move != "" && move != c.WorkspaceID {
			if err := tx.Model(&c).UpdateColumn("workspace_id", move).Error; err != nil {
				return err
			}
			c.WorkspaceID = move
		}
		if args.ShownIn == nil {
			// a conversation is not shown in the workspace it belongs to
			return rows.Session(&gorm.Session{}).Where("workspace_id = ?", own).Delete(&conversation.ConversationWorkspace{}).Error
		}
		shown = slices.DeleteFunc(shown, func(id string) bool { return id == own })
		del := rows.Session(&gorm.Session{})
		if len(shown) > 0 {
			del = del.Where("workspace_id NOT IN ?", shown)
		}
		if err := del.Delete(&conversation.ConversationWorkspace{}).Error; err != nil {
			return err
		}
		for _, id := range shown {
			row := conversation.ConversationWorkspace{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), ConversationID: c.ID, WorkspaceID: id}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fail(args.Fail(http.StatusInternalServerError, log.Wrap(err)))
	}
	places, err := s.placesOf(args.ServiceArgs, []conversation.Conversation{c}, ids)
	if err != nil {
		return fail(args.Fail(http.StatusInternalServerError, log.Wrap(err)))
	}
	return conversationservice.SetWorkspacesResponse{Common: features.OK(), Conversation: c, Places: places[c.ID]}
}
