package indexdto

import (
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ReindexResponse struct {
	rest.Status
	Queued int `json:"queued"`
}

// ReindexResponseFrom converts the service's ReindexResponse; its code is
// carried by the HTTP status rather than the body.
func ReindexResponseFrom(resp indexservice.ReindexResponse) ReindexResponse {
	return ReindexResponse{Status: rest.StatusFrom(resp.Common), Queued: resp.Queued}
}
