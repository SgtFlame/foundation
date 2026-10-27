package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/db"
	"gorm.io/gorm"
)

// roundText is how much of a linked round's message Get returns.
const roundText = 200

// Get returns one of the principal's entries with its links and the rounds
// that made or changed it, or an entry shared with them on its own.
func (s *service) Get(args chronicleservice.GetArgs) chronicleservice.GetResponse {
	if failed := check(args.ServiceArgs, "chronicle.read", args.ID); failed != nil {
		return chronicleservice.GetResponse{Common: *failed}
	}
	entry, failed := s.findVisible(args.ServiceArgs, args.ID)
	if failed != nil {
		return chronicleservice.GetResponse{Common: *failed}
	}
	resp := chronicleservice.GetResponse{Common: features.OK(), Entry: entry}
	if !mine(args.Principal, entry) {
		return resp // someone else's shared entry: its links and rounds are theirs
	}
	fail := func(err error) chronicleservice.GetResponse {
		return chronicleservice.GetResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if err := s.owned(args.ServiceArgs).Where("from_id = ? OR to_id = ?", entry.ID, entry.ID).Order("id").Find(&resp.Links).Error; err != nil {
		return fail(err)
	}
	var made []chronicle.EntryRound
	tenant := func() *gorm.DB { return s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)) }
	if err := tenant().Where("entry_id = ?", entry.ID).Order("id DESC").Find(&made).Error; err != nil {
		return fail(err)
	}
	if len(made) == 0 {
		return resp
	}
	roundIDs := make([]uint64, len(made))
	for i, m := range made {
		roundIDs[i] = m.RoundID
	}
	// Only the principal's own rounds and conversations are named
	var rounds []conversation.Round
	if err := chronicle.Owned(tenant(), args.Principal).Where("id IN ?", roundIDs).Find(&rounds).Error; err != nil {
		return fail(err)
	}
	texts := map[uint64]string{}
	conversationIDs := []uint64{}
	for _, r := range rounds {
		texts[r.ID] = clip(r.Text, roundText)
		conversationIDs = append(conversationIDs, r.ConversationID)
	}
	var conversations []conversation.Conversation
	if err := chronicle.Owned(tenant(), args.Principal).Where("id IN ?", conversationIDs).Find(&conversations).Error; err != nil {
		return fail(err)
	}
	titles := map[uint64]string{}
	for _, c := range conversations {
		titles[c.ID] = c.Title
	}
	for _, m := range made {
		resp.Rounds = append(resp.Rounds, chronicleservice.LinkedRound{EntryRound: m, Text: texts[m.RoundID], ConversationTitle: titles[m.ConversationID]})
	}
	return resp
}
