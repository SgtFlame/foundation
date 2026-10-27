package impl

import (
	"errors"
	"net/http"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"gorm.io/gorm"
)

var errAlreadyShared = errors.New("the entry is already shared globally, or waiting for approval")

// Share shares one of the principal's active entries, or stops sharing it.
func (s *service) Share(args chronicleservice.ShareArgs) chronicleservice.ShareResponse {
	if failed := check(args.ServiceArgs, "chronicle.share", args.ID); failed != nil {
		return chronicleservice.ShareResponse{Common: *failed}
	}
	entry, failed := s.find(args.ServiceArgs, args.ID)
	if failed != nil {
		return chronicleservice.ShareResponse{Common: *failed}
	}
	if entry.Status != chronicle.StatusActive {
		return chronicleservice.ShareResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("entry_not_active").Public("Accept the proposed entry before sharing it").New("a proposed entry cannot be shared"))}
	}
	resp := chronicleservice.ShareResponse{Common: features.OK()}
	var copies []uint64
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		switch args.Target {
		case chronicle.SharedTenant:
			entry.Shared = chronicle.SharedTenant
			return tx.Model(&entry).UpdateColumn("shared", entry.Shared).Error
		case "":
			var err error
			if copies, err = s.unshareGlobal(args.ServiceArgs, tx, entry.ID); err != nil {
				return err
			}
			entry.Shared = ""
			return tx.Model(&entry).UpdateColumn("shared", "").Error
		case chronicle.SharedGlobal:
			var open int64
			if err := tx.Model(&chronicle.ChronicleShare{}).Where("entry_id = ? AND status IN ?", entry.ID,
				[]string{chronicle.SharePending, chronicle.ShareTenantApproved, chronicle.ShareApproved}).Count(&open).Error; err != nil {
				return err
			}
			if open > 0 {
				return errAlreadyShared
			}
			share := chronicle.ChronicleShare{
				Tenanted: entry.Tenanted, Owned: entry.Owned, EntryID: entry.ID, Status: chronicle.SharePending,
				OwnerName: s.nameOf(entry.OwnerID), Type: entry.Type, Title: entry.Title,
				ShortSummary: entry.ShortSummary, FullSummary: entry.FullSummary, Note: strings.TrimSpace(args.Note),
			}
			if err := tx.Create(&share).Error; err != nil {
				return err
			}
			resp.Share = &share
			return nil
		}
		return errBadTarget
	})
	switch {
	case errors.Is(err, errBadTarget):
		return chronicleservice.ShareResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("invalid_share").Errorf("cannot share with %q", args.Target))}
	case errors.Is(err, errAlreadyShared):
		return chronicleservice.ShareResponse{Common: *args.Fail(http.StatusConflict, log.Code("already_shared").Public("This entry is already shared globally or waiting for approval").Wrap(err))}
	case err != nil:
		return chronicleservice.ShareResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	s.index(args.ServiceArgs, entry.ID) // the entries table says who may find it
	for _, id := range copies {
		s.index(args.ServiceArgs, id)
	}
	resp.Entry = entry
	return resp
}

var errBadTarget = errors.New("unknown share target")

// unshareGlobal withdraws the entry's open global requests and deletes its
// approved copy inside tx, returning the deleted copies to unindex.
func (s *service) unshareGlobal(args features.ServiceArgs, tx *gorm.DB, entryID uint64) ([]uint64, error) {
	var shares []chronicle.ChronicleShare
	if err := tx.Where("entry_id = ? AND status IN ?", entryID,
		[]string{chronicle.SharePending, chronicle.ShareTenantApproved, chronicle.ShareApproved}).Find(&shares).Error; err != nil {
		return nil, err
	}
	var copies []uint64
	for _, share := range shares {
		if share.CopyID != 0 {
			if err := tx.Where("tenant_id = ?", features.GlobalTenantID).Delete(&chronicle.Entry{}, share.CopyID).Error; err != nil {
				return nil, err
			}
			copies = append(copies, share.CopyID)
		}
		if err := tx.Model(&share).UpdateColumn("status", chronicle.ShareWithdrawn).Error; err != nil {
			return nil, err
		}
	}
	return copies, nil
}

// nameOf is how a share names its owner.
func (s *service) nameOf(user uint64) string {
	if s.userName == nil {
		return ""
	}
	return s.userName(user)
}
