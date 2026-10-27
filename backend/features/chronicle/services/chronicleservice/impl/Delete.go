package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"gorm.io/gorm"
)

// Delete removes one of the principal's entries: its children move up to
// its parent, and its links, round records and global shares go with it.
func (s *service) Delete(args chronicleservice.DeleteArgs) chronicleservice.DeleteResponse {
	if failed := check(args.ServiceArgs, "chronicle.write", args.ID); failed != nil {
		return chronicleservice.DeleteResponse{Common: *failed}
	}
	entry, failed := s.find(args.ServiceArgs, args.ID)
	if failed != nil {
		return chronicleservice.DeleteResponse{Common: *failed}
	}
	var copies []uint64
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		owned := func() *gorm.DB { return chronicle.Owned(tx, args.Principal) }
		if err := owned().Model(&chronicle.Entry{}).Where("parent_id = ?", entry.ID).Update("parent_id", entry.ParentID).Error; err != nil {
			return err
		}
		if err := owned().Where("from_id = ? OR to_id = ?", entry.ID, entry.ID).Delete(&chronicle.EntryLink{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND entry_id = ?", entry.TenantID, entry.ID).Delete(&chronicle.EntryRound{}).Error; err != nil {
			return err
		}
		var err error
		if copies, err = s.unshareGlobal(args.ServiceArgs, tx, entry.ID); err != nil {
			return err
		}
		return tx.Delete(&entry).Error
	})
	if err != nil {
		return chronicleservice.DeleteResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	s.index(args.ServiceArgs, entry.ID) // removes it from the entries table
	for _, id := range copies {
		s.index(args.ServiceArgs, id)
	}
	return chronicleservice.DeleteResponse{Common: features.OK()}
}
