package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ModerateResponse struct {
	rest.Status
	Share *Share `json:"share,omitempty"`
}

// ModerateResponseFrom converts the service's ModerateResponse; its code is
// carried by the HTTP status rather than the body.
func ModerateResponseFrom(resp chronicleservice.ModerateResponse) ModerateResponse {
	out := ModerateResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Common.Code < 300 {
		s := ShareFrom(resp.Share)
		out.Share = &s
	}
	return out
}
