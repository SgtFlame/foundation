package bootstrapdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
)

type StatusResponse struct {
	rest.Status
	State string `json:"state"`
}

// StatusResponseFrom converts the service's StatusResponse; its code is
// carried by the HTTP status rather than the body.
func StatusResponseFrom(resp bootstrapservice.StatusResponse) StatusResponse {
	return StatusResponse{Status: rest.StatusFrom(resp.Common), State: string(resp.State)}
}
