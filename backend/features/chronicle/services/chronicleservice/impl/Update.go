package impl

import (
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"gorm.io/gorm"
)

// Update replaces what the user edits of one of the principal's entries,
// accepting it or clearing its review flag when asked, and indexes it again
// in the background.
func (s *service) Update(args chronicleservice.UpdateArgs) chronicleservice.EntryResponse {
	if failed := check(args.ServiceArgs, "chronicle.write", args.ID); failed != nil {
		return chronicleservice.EntryResponse{Common: *failed}
	}
	entry, failed := s.find(args.ServiceArgs, args.ID)
	if failed != nil {
		return chronicleservice.EntryResponse{Common: *failed}
	}
	before := entry
	if failed := s.apply(args.ServiceArgs, args.EntryInput, &entry); failed != nil {
		return chronicleservice.EntryResponse{Common: *failed}
	}
	if args.Accept {
		entry.Status = chronicle.StatusActive
	}
	if args.Reviewed {
		entry.Review = false
	}
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&entry).Error; err != nil {
			return err
		}
		if args.RoundID == 0 || (before.ShortSummary == entry.ShortSummary && before.FullSummary == entry.FullSummary) {
			return nil
		}
		previous, _ := json.Marshal(map[string]string{"shortSummary": before.ShortSummary, "fullSummary": before.FullSummary})
		return tx.Create(&chronicle.EntryRound{Tenanted: entry.Tenanted, EntryID: entry.ID, RoundID: args.RoundID, ConversationID: args.ConversationID,
			Action: chronicle.ActionUpdated, Previous: string(previous)}).Error
	})
	if err != nil {
		return chronicleservice.EntryResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	s.index(args.ServiceArgs, entry.ID)
	return chronicleservice.EntryResponse{Common: features.OK(), Entry: entry}
}
