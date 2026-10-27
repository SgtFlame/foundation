package configdto

import (
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ResetResponse struct {
	rest.Status
	View
}

// ResetResponseFrom converts the service's ResetResponse; its code is carried
// by the HTTP status rather than the body.
func ResetResponseFrom(resp configservice.ResetResponse) ResetResponse {
	return ResetResponse{Status: rest.StatusFrom(resp.Common), View: ViewFrom(resp.View)}
}
