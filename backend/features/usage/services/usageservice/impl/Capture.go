package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
	"gorm.io/gorm"
)

// Capture reads the prompt and reply text captured for one usage record.
func (s *service) Capture(args usageservice.CaptureArgs) usageservice.CaptureResponse {
	const action = "usage.readCapture"
	resource := authz.Tenant(args.Principal) + "/" + authz.Resource("usage", args.RecordID)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return usageservice.CaptureResponse{Common: *args.Fail(authz.Status, err)}
	}
	var c usage.Capture
	err := s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).Where("record_id = ?", args.RecordID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return usageservice.CaptureResponse{Common: *args.Fail(http.StatusNotFound,
			log.Code("capture_not_found").With("recordId", args.RecordID).New("this call was not captured, or its capture has expired"))}
	}
	if err != nil {
		return usageservice.CaptureResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return usageservice.CaptureResponse{Common: features.OK(), Capture: c}
}
