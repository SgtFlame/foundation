package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ReindexResponse struct {
	rest.Status
	Indexed int `json:"indexed"`
}

// ReindexResponseFrom converts the service's ReindexResponse; its code is
// carried by the HTTP status rather than the body.
func ReindexResponseFrom(resp chronicleservice.ReindexResponse) ReindexResponse {
	return ReindexResponse{Status: rest.StatusFrom(resp.Common), Indexed: resp.Indexed}
}
