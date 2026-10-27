package bootstrapdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
)

type UnsealResponse struct {
	rest.Status
	State string `json:"state"`
}

// UnsealResponseFrom converts the service's UnsealResponse; its code is
// carried by the HTTP status rather than the body.
func UnsealResponseFrom(resp bootstrapservice.UnsealResponse) UnsealResponse {
	return UnsealResponse{Status: rest.StatusFrom(resp.Common), State: string(resp.State)}
}
