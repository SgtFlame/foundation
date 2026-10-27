package impl

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"gorm.io/gorm"
)

var errNotWaiting = errors.New("the request is not waiting for moderation")

// Moderate approves or rejects a share request at its stage. The two
// approvals come from two people: whoever approved for the tenant may still
// reject at the global stage, but not approve it.
func (s *service) Moderate(args chronicleservice.ModerateArgs) chronicleservice.ModerateResponse {
	var share chronicle.ChronicleShare
	err := s.db.WithContext(args.Context()).First(&share, args.ShareID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return chronicleservice.ModerateResponse{Common: *args.Fail(http.StatusNotFound, log.Code("share_not_found").With("shareId", args.ShareID).New("there is no such share request"))}
	}
	if err != nil {
		return chronicleservice.ModerateResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	var action, res string
	switch share.Status {
	case chronicle.SharePending:
		action, res = "chronicle.moderate", authz.Resource("tenant", share.TenantID)
		if share.TenantID != args.Principal.TenantID {
			return chronicleservice.ModerateResponse{Common: *args.Fail(http.StatusNotFound, log.Code("share_not_found").With("shareId", args.ShareID).New("the request is another tenant's"))}
		}
	case chronicle.ShareTenantApproved:
		action, res = "chronicle.moderateGlobal", authz.OnApp
	default:
		return chronicleservice.ModerateResponse{Common: *args.Fail(http.StatusConflict, log.Code("share_decided").Public("This request has already been decided").Wrap(errNotWaiting))}
	}
	if err := authz.Check(args.Context(), args.Principal, action, res).Err(action, res); err != nil {
		return chronicleservice.ModerateResponse{Common: *args.Fail(authz.Status, err)}
	}
	if share.TenantID == args.Principal.TenantID && share.OwnerID == args.Principal.UserID {
		return chronicleservice.ModerateResponse{Common: *args.Fail(authz.Status,
			log.Code("own_share").Public("Someone else must approve your own share").New("a moderator tried to moderate their own share"))}
	}
	if share.Status == chronicle.ShareTenantApproved && share.TenantBy == args.Principal.UserID && args.Approve {
		return chronicleservice.ModerateResponse{Common: *args.Fail(authz.Status,
			log.Code("second_approval").Public("Another moderator must give the global approval").New("one moderator tried to give both approvals"))}
	}

	now := time.Now()
	var copyID uint64
	err = s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{}
		switch {
		case !args.Approve:
			updates["status"], updates["note"] = chronicle.ShareRejected, strings.TrimSpace(args.Note)
		case share.Status == chronicle.SharePending:
			updates["status"] = chronicle.ShareTenantApproved
		default:
			// The global tenant's copy, made from what the moderators read
			c := chronicle.Entry{
				Tenanted: db.Tenanted{TenantID: features.GlobalTenantID}, Owned: db.Owned{OwnerID: share.OwnerID},
				Type: share.Type, Title: share.Title, ShortSummary: share.ShortSummary, FullSummary: share.FullSummary,
				Fields: "{}", Status: chronicle.StatusActive, Shared: chronicle.SharedGlobal, SourceID: share.EntryID,
			}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			copyID = c.ID
			updates["status"], updates["copy_id"] = chronicle.ShareApproved, c.ID
		}
		if share.Status == chronicle.SharePending {
			updates["tenant_by"], updates["tenant_at"] = args.Principal.UserID, now
		} else {
			updates["global_by"], updates["global_at"] = args.Principal.UserID, now
		}
		res := tx.Model(&chronicle.ChronicleShare{}).Where("id = ? AND status = ?", share.ID, share.Status).Updates(updates)
		if res.Error == nil && res.RowsAffected == 0 {
			return errNotWaiting // decided by someone else meanwhile
		}
		return res.Error
	})
	switch {
	case errors.Is(err, errNotWaiting):
		return chronicleservice.ModerateResponse{Common: *args.Fail(http.StatusConflict, log.Code("share_decided").Public("This request has already been decided").Wrap(err))}
	case err != nil:
		return chronicleservice.ModerateResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if copyID != 0 {
		s.index(args.ServiceArgs, copyID)
	}
	s.db.WithContext(args.Context()).First(&share, share.ID)
	log.With("shareId", share.ID, "status", share.Status).Info(args.Context(), "share request %s", share.Status)
	return chronicleservice.ModerateResponse{Common: features.OK(), Share: share}
}
